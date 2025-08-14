package discord

import (
	"context"
	"fmt"
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

	// Register message handler
	session.AddHandler(bot.messageHandler)

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
	// Find the chain config to get the logo
	var chainConfig *config.ChainConfig
	for _, chain := range b.config.Chains {
		if chain.ChainID == proposal.ChainID {
			chainConfig = &chain
			break
		}
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
				Value:  fmt.Sprintf("`%s`", proposal.ChainID),
				Inline: true,
			},
			{
				Name:   "📊 Status",
				Value:  b.formatStatus(proposal.Status),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Use: !vote %s %s <yes/no/abstain/no_with_veto> <secret>", proposal.ChainID, proposal.ProposalID),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Add chain logo if available
	if chainConfig != nil && chainConfig.LogoURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: chainConfig.LogoURL,
		}
		if chainConfig.Name != "" {
			embed.Author = &discordgo.MessageEmbedAuthor{
				Name:    chainConfig.Name,
				IconURL: chainConfig.LogoURL,
			}
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

	b.sendEmbed(b.config.Discord.ChannelID, embed)

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
