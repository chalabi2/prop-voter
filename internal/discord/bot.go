package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"prop-voter/config"
	"prop-voter/internal/models"
	"prop-voter/internal/voting"

	"github.com/bwmarrin/discordgo"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Bot represents the Discord bot
type Bot struct {
	session    *discordgo.Session
	db         *gorm.DB
	config     *config.Config
	logger     *zap.Logger
	voter      *voting.Voter
	notifyChan chan models.Proposal
}

// NewBot creates a new Discord bot instance
func NewBot(db *gorm.DB, config *config.Config, logger *zap.Logger, voter *voting.Voter) (*Bot, error) {
	session, err := discordgo.New("Bot " + config.Discord.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	bot := &Bot{
		session:    session,
		db:         db,
		config:     config,
		logger:     logger,
		voter:      voter,
		notifyChan: make(chan models.Proposal, 100),
	}

	// Register message and interaction handlers
	session.AddHandler(bot.messageHandler)
	session.AddHandler(bot.interactionHandler)

	return bot, nil
}

// Start starts the Discord bot
func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info("Starting Discord bot")

	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord session: %w", err)
	}

	// Start notification goroutine
	go b.handleNotifications(ctx)

	// Start periodic notification check
	go b.checkForNewProposals(ctx)

	return nil
}

// Stop stops the Discord bot
func (b *Bot) Stop() error {
	b.logger.Info("Stopping Discord bot")
	close(b.notifyChan)
	return b.session.Close()
}

// messageHandler handles incoming Discord messages
func (b *Bot) messageHandler(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Only respond to messages from the allowed user
	if m.Author.ID != b.config.Discord.AllowedUser {
		b.logger.Warn("Unauthorized user attempted to use bot",
			zap.String("user_id", m.Author.ID),
			zap.String("username", m.Author.Username),
		)
		return
	}

	// Only respond to messages in the configured channel
	if m.ChannelID != b.config.Discord.ChannelID {
		return
	}

	content := strings.TrimSpace(m.Content)
	parts := strings.Fields(content)

	if len(parts) == 0 {
		return
	}

	command := strings.ToLower(parts[0])

	switch command {
	case "!help":
		b.sendHelp(m.ChannelID)
	case "!proposals":
		b.listProposals(m.ChannelID, parts[1:])
	case "!vote":
		b.handleVoteCommand(m.ChannelID, parts[1:])
	case "!status":
		b.showStatus(m.ChannelID, parts[1:])
	default:
		if strings.HasPrefix(content, "!") {
			b.sendMessage(m.ChannelID, "Unknown command. Type `!help` for available commands.")
		}
	}
}

// sendHelp sends help information
func (b *Bot) sendHelp(channelID string) {
	help := `**Prop-Voter Bot Commands:**

` + "`" + `!help` + "`" + ` - Show this help message
` + "`" + `!proposals [chain]` + "`" + ` - List recent proposals (optionally filter by chain)
` + "`" + `!vote <chain> <proposal_id> <vote> <secret>` + "`" + ` - Vote on a proposal
  - vote options: yes, no, abstain, no_with_veto
  - secret: your configured vote secret
` + "`" + `!status [chain] [proposal_id]` + "`" + ` - Show voting status

**Examples:**
` + "`" + `!proposals cosmoshub-4` + "`" + `
` + "`" + `!vote cosmoshub-4 123 yes mysecret` + "`" + `
` + "`" + `!status cosmoshub-4 123` + "`" + ``

	b.sendMessage(channelID, help)
}

// listProposals lists recent proposals
func (b *Bot) listProposals(channelID string, args []string) {
	var proposals []models.Proposal
	query := b.db.Order("created_at DESC").Limit(10)

	if len(args) > 0 {
		chainFilter := args[0]
		query = query.Where("chain_id = ?", chainFilter)
	}

	if err := query.Find(&proposals).Error; err != nil {
		b.sendMessage(channelID, "❌ Failed to fetch proposals")
		return
	}

	if len(proposals) == 0 {
		b.sendMessage(channelID, "No proposals found")
		return
	}

	var message strings.Builder
	message.WriteString("**Recent Proposals:**\n\n")

	for _, proposal := range proposals {
		message.WriteString(fmt.Sprintf("**%s - Proposal #%s**\n", proposal.ChainID, proposal.ProposalID))
		message.WriteString(fmt.Sprintf("Title: %s\n", proposal.Title))
		message.WriteString(fmt.Sprintf("Status: %s\n", proposal.Status))

		if proposal.VotingEnd != nil {
			message.WriteString(fmt.Sprintf("Voting Ends: %s\n", proposal.VotingEnd.Format(time.RFC3339)))
		}

		message.WriteString("\n")
	}

	b.sendMessage(channelID, message.String())
}

// handleVoteCommand handles vote commands
func (b *Bot) handleVoteCommand(channelID string, args []string) {
	if len(args) < 4 {
		b.sendMessage(channelID, "❌ Usage: `!vote <chain> <proposal_id> <vote> <secret>`")
		return
	}

	chainID := args[0]
	proposalID := args[1]
	voteOption := strings.ToLower(args[2])
	secret := args[3]

	// Verify secret
	if secret != b.config.Security.VoteSecret {
		b.sendMessage(channelID, "❌ Invalid secret")
		b.logger.Warn("Invalid vote secret provided",
			zap.String("chain", chainID),
			zap.String("proposal", proposalID),
		)
		return
	}

	// Validate vote option
	validVotes := map[string]bool{
		"yes":          true,
		"no":           true,
		"abstain":      true,
		"no_with_veto": true,
	}

	if !validVotes[voteOption] {
		b.sendMessage(channelID, "❌ Invalid vote option. Use: yes, no, abstain, no_with_veto")
		return
	}

	// Check if proposal exists
	var proposal models.Proposal
	if err := b.db.Where("chain_id = ? AND proposal_id = ?", chainID, proposalID).First(&proposal).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			b.sendMessage(channelID, "❌ Proposal not found")
		} else {
			b.sendMessage(channelID, "❌ Database error")
		}
		return
	}

	b.sendMessage(channelID, fmt.Sprintf("🗳️ Submitting vote: %s on %s proposal #%s...", voteOption, chainID, proposalID))

	// Submit vote
	txHash, err := b.voter.Vote(chainID, proposalID, voteOption)
	if err != nil {
		b.sendMessage(channelID, fmt.Sprintf("❌ Failed to vote: %s", err.Error()))
		return
	}

	// Store vote in database
	vote := models.Vote{
		ChainID:    chainID,
		ProposalID: proposalID,
		Option:     voteOption,
		TxHash:     txHash,
		VotedAt:    time.Now(),
	}

	if err := b.db.Create(&vote).Error; err != nil {
		b.logger.Error("Failed to store vote", zap.Error(err))
	}

	b.sendMessage(channelID, fmt.Sprintf("✅ Vote submitted successfully!\nTx Hash: %s", txHash))
}

// showStatus shows voting status for a proposal
func (b *Bot) showStatus(channelID string, args []string) {
	if len(args) < 2 {
		b.sendMessage(channelID, "❌ Usage: `!status <chain> <proposal_id>`")
		return
	}

	chainID := args[0]
	proposalID := args[1]

	var proposal models.Proposal
	if err := b.db.Preload("Vote").Where("chain_id = ? AND proposal_id = ?", chainID, proposalID).First(&proposal).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			b.sendMessage(channelID, "❌ Proposal not found")
		} else {
			b.sendMessage(channelID, "❌ Database error")
		}
		return
	}

	var message strings.Builder
	message.WriteString(fmt.Sprintf("**%s - Proposal #%s Status**\n\n", chainID, proposalID))
	message.WriteString(fmt.Sprintf("Title: %s\n", proposal.Title))
	message.WriteString(fmt.Sprintf("Status: %s\n", proposal.Status))

	if proposal.VotingEnd != nil {
		message.WriteString(fmt.Sprintf("Voting Ends: %s\n", proposal.VotingEnd.Format(time.RFC3339)))
	}

	if proposal.Vote != nil {
		message.WriteString(fmt.Sprintf("\n**Your Vote:** %s\n", proposal.Vote.Option))
		message.WriteString(fmt.Sprintf("Voted At: %s\n", proposal.Vote.VotedAt.Format(time.RFC3339)))
		message.WriteString(fmt.Sprintf("Tx Hash: %s\n", proposal.Vote.TxHash))
	} else {
		message.WriteString("\n**Your Vote:** Not voted yet")
	}

	b.sendMessage(channelID, message.String())
}

// sendMessage sends a message to a Discord channel
func (b *Bot) sendMessage(channelID, content string) {
	if _, err := b.session.ChannelMessageSend(channelID, content); err != nil {
		b.logger.Error("Failed to send Discord message", zap.Error(err))
	}
}

// checkForNewProposals periodically checks for new proposals to notify about
func (b *Bot) checkForNewProposals(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var proposals []models.Proposal
			if err := b.db.Where("notification_sent = ?", false).Find(&proposals).Error; err != nil {
				b.logger.Error("Failed to fetch unnotified proposals", zap.Error(err))
				continue
			}

			for _, proposal := range proposals {
				select {
				case b.notifyChan <- proposal:
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

// handleNotifications handles proposal notifications
func (b *Bot) handleNotifications(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case proposal := <-b.notifyChan:
			b.sendProposalNotification(proposal)
		}
	}
}

// sendProposalNotification sends a notification about a new proposal
func (b *Bot) sendProposalNotification(proposal models.Proposal) {
	b.logger.Debug("Sending proposal notification",
		zap.String("chain_id", proposal.ChainID),
		zap.String("proposal_id", proposal.ProposalID),
		zap.String("title", proposal.Title),
	)

	// Find the chain config to get the logo and metadata
	var chainConfig *config.ChainConfig
	for i, chain := range b.config.Chains {
		// Use GetChainID() to support both legacy and Chain Registry formats
		if chain.GetChainID() == proposal.ChainID {
			chainConfig = &b.config.Chains[i]
			b.logger.Debug("Found matching chain config",
				zap.String("chain_name", chain.GetName()),
				zap.String("chain_id", chain.GetChainID()),
			)
			break
		}
	}

	if chainConfig == nil {
		b.logger.Warn("No chain config found for proposal",
			zap.String("proposal_chain_id", proposal.ChainID),
			zap.String("proposal_id", proposal.ProposalID),
		)
	}

	// Determine chain name and logo (works for both legacy and Chain Registry)
	chainName := proposal.ChainID // Fallback to chain ID
	chainLogoURL := ""

	if chainConfig != nil {
		// Get chain name and logo using helper methods
		chainName = chainConfig.GetName()
		chainLogoURL = chainConfig.GetLogoURL()
	}

	// Create rich embed
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🗳️ New Proposal #%s", proposal.ProposalID),
		Color: b.getStatusColor(proposal.Status),
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "📋 Title",
				Value:  proposal.Title,
				Inline: false,
			},
			{
				Name:   "⛓️ Chain",
				Value:  fmt.Sprintf("**%s** (`%s`)", chainName, proposal.ChainID),
				Inline: true,
			},
			{
				Name:   "📊 Status",
				Value:  b.formatStatus(proposal.Status),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Vote: !vote %s %s <yes/no/abstain/no_with_veto> <secret> • Chain: %s", proposal.ChainID, proposal.ProposalID, chainName),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Add chain logo and author info
	if chainLogoURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: chainLogoURL,
		}
		embed.Author = &discordgo.MessageEmbedAuthor{
			Name:    chainName,
			IconURL: chainLogoURL,
		}
	}

	// Add voting deadline if available
	if proposal.VotingEnd != nil {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "⏰ Voting Ends",
			Value:  fmt.Sprintf("<t:%d:R>", proposal.VotingEnd.Unix()),
			Inline: true,
		})
	}

	// Add description if available and not too long
	if proposal.Description != "" {
		description := proposal.Description
		if len(description) > 300 {
			description = description[:297] + "..."
		}
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "📝 Description",
			Value:  description,
			Inline: false,
		})
	}

	// Send embed with interactive vote tally button
	b.sendEmbedWithButtons(b.config.Discord.ChannelID, embed, proposal)

	// Mark notification as sent
	proposal.NotificationSent = true
	if err := b.db.Save(&proposal).Error; err != nil {
		b.logger.Error("Failed to mark notification as sent", zap.Error(err))
	}
}

// getStatusColor returns a color code based on proposal status
func (b *Bot) getStatusColor(status string) int {
	switch {
	case strings.Contains(status, "VOTING"):
		return 0x3498db // Blue for active voting
	case strings.Contains(status, "PASSED"):
		return 0x2ecc71 // Green for passed
	case strings.Contains(status, "REJECTED") || strings.Contains(status, "FAILED"):
		return 0xe74c3c // Red for rejected/failed
	case strings.Contains(status, "DEPOSIT"):
		return 0xf39c12 // Orange for deposit period
	default:
		return 0x95a5a6 // Gray for unknown
	}
}

// formatStatus formats the proposal status with emojis
func (b *Bot) formatStatus(status string) string {
	switch {
	case strings.Contains(status, "VOTING"):
		return "🔴 **VOTING PERIOD**"
	case strings.Contains(status, "PASSED"):
		return "✅ **PASSED**"
	case strings.Contains(status, "REJECTED"):
		return "❌ **REJECTED**"
	case strings.Contains(status, "FAILED"):
		return "💥 **FAILED**"
	case strings.Contains(status, "DEPOSIT"):
		return "💰 **DEPOSIT PERIOD**"
	default:
		return fmt.Sprintf("📊 %s", status)
	}
}

// sendEmbed sends a Discord embed message
func (b *Bot) sendEmbed(channelID string, embed *discordgo.MessageEmbed) {
	_, err := b.session.ChannelMessageSendEmbed(channelID, embed)
	if err != nil {
		b.logger.Error("Failed to send embed message",
			zap.String("channel", channelID),
			zap.Error(err),
		)
	}
}

// sendEmbedWithButtons sends a Discord embed with interactive buttons
func (b *Bot) sendEmbedWithButtons(channelID string, embed *discordgo.MessageEmbed, proposal models.Proposal) {
	// Create vote tally button
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "📊 Check Vote Tally",
					Style:    discordgo.SecondaryButton,
					CustomID: fmt.Sprintf("vote_tally_%s_%s", proposal.ChainID, proposal.ProposalID),
					Emoji: discordgo.ComponentEmoji{
						Name: "📊",
					},
				},
			},
		},
	}

	data := &discordgo.MessageSend{
		Embed:      embed,
		Components: components,
	}

	_, err := b.session.ChannelMessageSendComplex(channelID, data)
	if err != nil {
		b.logger.Error("Failed to send embed with buttons",
			zap.String("channel", channelID),
			zap.Error(err),
		)
	}
}

// interactionHandler handles Discord button interactions
func (b *Bot) interactionHandler(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Only handle button interactions
	if i.Type != discordgo.InteractionMessageComponent {
		return
	}

	// Check if this is a vote tally button
	if strings.HasPrefix(i.MessageComponentData().CustomID, "vote_tally_") {
		b.handleVoteTallyButton(s, i)
	}
}

// handleVoteTallyButton handles vote tally button clicks
func (b *Bot) handleVoteTallyButton(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Extract chain ID and proposal ID from custom ID
	// Format: vote_tally_{chainID}_{proposalID}
	// But chainID might contain underscores, so we need to be careful
	customID := i.MessageComponentData().CustomID

	// Remove the "vote_tally_" prefix
	if !strings.HasPrefix(customID, "vote_tally_") {
		b.respondWithError(s, i, "Invalid button format")
		return
	}

	remainder := strings.TrimPrefix(customID, "vote_tally_")

	// Find the last underscore to separate proposal ID
	lastUnderscoreIndex := strings.LastIndex(remainder, "_")
	if lastUnderscoreIndex == -1 {
		b.respondWithError(s, i, "Invalid button data format")
		return
	}

	chainID := remainder[:lastUnderscoreIndex]
	proposalID := remainder[lastUnderscoreIndex+1:]

	// Find the chain config
	var chainConfig *config.ChainConfig
	for idx, chain := range b.config.Chains {
		if chain.GetChainID() == chainID {
			chainConfig = &b.config.Chains[idx]
			break
		}
	}

	if chainConfig == nil {
		b.respondWithError(s, i, fmt.Sprintf("Chain configuration not found for %s", chainID))
		return
	}

	// Defer the response to give us time to query the chain
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		b.logger.Error("Failed to defer interaction response", zap.Error(err))
		return
	}

	// Query the vote tally
	tally, err := b.queryVoteTally(chainConfig, proposalID)
	if err != nil {
		b.followupWithError(s, i, fmt.Sprintf("Failed to query vote tally: %v", err))
		return
	}

	// Create response embed
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("📊 Vote Tally - Proposal #%s", proposalID),
		Color: 0x3498db, // Blue color
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "✅ Yes",
				Value:  tally.Yes,
				Inline: true,
			},
			{
				Name:   "❌ No",
				Value:  tally.No,
				Inline: true,
			},
			{
				Name:   "🤷 Abstain",
				Value:  tally.Abstain,
				Inline: true,
			},
			{
				Name:   "🚫 No with Veto",
				Value:  tally.NoWithVeto,
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Chain: %s • Updated: %s", chainConfig.GetName(), time.Now().Format("15:04:05")),
		},
	}

	// Send the follow-up response
	_, err = s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Embeds: []*discordgo.MessageEmbed{embed},
	})
	if err != nil {
		b.logger.Error("Failed to send vote tally response", zap.Error(err))
	}
}

// VoteTally represents vote tally results
type VoteTally struct {
	Yes        string `json:"yes"`
	No         string `json:"no"`
	Abstain    string `json:"abstain"`
	NoWithVeto string `json:"no_with_veto"`
}

// queryVoteTally queries the chain for vote tally results
func (b *Bot) queryVoteTally(chainConfig *config.ChainConfig, proposalID string) (*VoteTally, error) {
	baseURL := strings.TrimSuffix(chainConfig.REST, "/")

	// Try different API versions - newer chains might use v1, older ones v1beta1
	apiVersions := []string{"v1", "v1beta1"}

	for _, version := range apiVersions {
		url := fmt.Sprintf("%s/cosmos/gov/%s/proposals/%s/tally", baseURL, version, proposalID)

		b.logger.Info("Querying vote tally",
			zap.String("chain", chainConfig.GetName()),
			zap.String("proposal", proposalID),
			zap.String("api_version", version),
			zap.String("url", url),
		)

		tally, err := b.tryQueryTally(url, version)
		if err != nil {
			b.logger.Debug("API version failed, trying next",
				zap.String("version", version),
				zap.Error(err),
			)
			continue
		}

		b.logger.Info("Successfully retrieved vote tally",
			zap.String("chain", chainConfig.GetName()),
			zap.String("api_version", version),
		)

		// Convert raw amounts to human-readable format
		return &VoteTally{
			Yes:        b.formatTokenAmount(tally.Yes, chainConfig),
			No:         b.formatTokenAmount(tally.No, chainConfig),
			Abstain:    b.formatTokenAmount(tally.Abstain, chainConfig),
			NoWithVeto: b.formatTokenAmount(tally.NoWithVeto, chainConfig),
		}, nil
	}

	return nil, fmt.Errorf("failed to query vote tally using any API version")
}

// TallyResponse represents the raw tally data from the API
type TallyResponse struct {
	Yes        string `json:"yes_count"`
	No         string `json:"no_count"`
	Abstain    string `json:"abstain_count"`
	NoWithVeto string `json:"no_with_veto_count"`
}

// tryQueryTally attempts to query the tally using a specific API version
func (b *Bot) tryQueryTally(url, version string) (*TallyResponse, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Read response body for debugging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	b.logger.Debug("API response received",
		zap.String("api_version", version),
		zap.Int("status_code", resp.StatusCode),
		zap.String("response_body", string(body)),
	)

	// Parse based on API version
	if version == "v1" {
		// Gov v1 API format
		var response struct {
			Tally *TallyResponse `json:"tally"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to decode v1 response: %w", err)
		}
		if response.Tally == nil {
			return nil, fmt.Errorf("tally field is null in v1 response")
		}
		return response.Tally, nil
	} else {
		// Gov v1beta1 API format
		var response struct {
			Tally TallyResponse `json:"tally"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to decode v1beta1 response: %w", err)
		}
		return &response.Tally, nil
	}
}

// formatTokenAmount converts raw token amounts to human-readable format
func (b *Bot) formatTokenAmount(amount string, chainConfig *config.ChainConfig) string {
	if amount == "" || amount == "0" {
		return "0"
	}

	// Convert string to float for formatting
	if value, err := strconv.ParseFloat(amount, 64); err == nil {
		// Get the correct decimal precision for this chain
		decimals := 6 // Default to 6 decimals
		if chainConfig.UsesChainRegistry() && chainConfig.RegistryInfo != nil {
			decimals = chainConfig.RegistryInfo.Decimals
		}

		// Convert from base units to main units using the chain's decimal precision
		divisor := 1.0
		for i := 0; i < decimals; i++ {
			divisor *= 10
		}
		value = value / divisor

		// Format with appropriate precision
		if value >= 1000000 {
			return fmt.Sprintf("%.2fM", value/1000000)
		} else if value >= 1000 {
			return fmt.Sprintf("%.2fK", value/1000)
		} else if value >= 1 {
			return fmt.Sprintf("%.2f", value)
		} else {
			return fmt.Sprintf("%.4f", value)
		}
	}

	// Fallback to raw amount if parsing fails
	return amount
}

// respondWithError sends an error response to an interaction
func (b *Bot) respondWithError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("❌ %s", message),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		b.logger.Error("Failed to send error response", zap.Error(err))
	}
}

// followupWithError sends an error follow-up message
func (b *Bot) followupWithError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	_, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Content: fmt.Sprintf("❌ %s", message),
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		b.logger.Error("Failed to send error follow-up", zap.Error(err))
	}
}
