package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
)

func main() {
	var (
		token     = flag.String("token", "", "Discord bot token")
		channelID = flag.String("channel", "", "Discord channel ID")
		userID    = flag.String("user", "", "Your Discord user ID")
	)
	flag.Parse()

	if *token == "" || *channelID == "" || *userID == "" {
		fmt.Println("Usage: go run test-discord.go -token YOUR_TOKEN -channel CHANNEL_ID -user YOUR_USER_ID")
		fmt.Println("\nThis script tests your Discord bot configuration before running the full prop-voter.")
		fmt.Println("It will:")
		fmt.Println("  1. Connect to Discord")
		fmt.Println("  2. Send a test message to your channel")
		fmt.Println("  3. Listen for your !test command")
		fmt.Println("  4. Verify permissions and access")
		os.Exit(1)
	}

	// Create Discord session
	dg, err := discordgo.New("Bot " + *token)
	if err != nil {
		log.Fatal("Error creating Discord session: ", err)
	}

	// Add message handler
	dg.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore messages from the bot itself
		if m.Author.ID == s.State.User.ID {
			return
		}

		// Only respond to the specified user
		if m.Author.ID != *userID {
			log.Printf("❌ Message from unauthorized user: %s (%s)", m.Author.Username, m.Author.ID)
			return
		}

		// Only respond in the specified channel
		if m.ChannelID != *channelID {
			log.Printf("❌ Message in wrong channel: %s (expected: %s)", m.ChannelID, *channelID)
			return
		}

		if m.Content == "!test" {
			response := fmt.Sprintf("✅ **Discord Test Successful!**\n\n"+
				"- Bot can receive messages from you\n"+
				"- Bot can send messages to this channel\n"+
				"- User authorization working: %s (%s)\n"+
				"- Channel access working: %s\n\n"+
				"Your Discord setup is ready for Prop-Voter! 🎉",
				m.Author.Username, m.Author.ID, m.ChannelID)

			if _, err := s.ChannelMessageSend(m.ChannelID, response); err != nil {
				log.Printf("❌ Error sending response: %v", err)
			} else {
				log.Printf("✅ Test response sent successfully!")
			}
		}
	})

	// Open connection
	err = dg.Open()
	if err != nil {
		log.Fatal("❌ Error opening Discord connection: ", err)
	}
	defer dg.Close()

	log.Printf("✅ Discord bot connected successfully!")
	log.Printf("Bot user: %s#%s (ID: %s)", dg.State.User.Username, dg.State.User.Discriminator, dg.State.User.ID)

	// Send initial test message
	initialMsg := fmt.Sprintf("🤖 **Prop-Voter Discord Test**\n\n"+
		"If you can see this message, the bot has basic send permissions!\n\n"+
		"**Next steps:**\n"+
		"1. Type `!test` to verify two-way communication\n"+
		"2. Only you should be able to trigger the bot\n"+
		"3. The bot should respond with a success message\n\n"+
		"Expected authorized user: <@%s>", *userID)

	if _, err := dg.ChannelMessageSend(*channelID, initialMsg); err != nil {
		log.Printf("❌ Error sending initial message: %v", err)
		log.Printf("This might indicate:")
		log.Printf("  - Wrong channel ID")
		log.Printf("  - Bot doesn't have 'Send Messages' permission")
		log.Printf("  - Bot isn't in the server")
	} else {
		log.Printf("✅ Initial test message sent!")
		log.Printf("Check your Discord channel and type !test")
	}

	log.Printf("\n🔍 Monitoring for messages...")
	log.Printf("Authorized user ID: %s", *userID)
	log.Printf("Target channel ID: %s", *channelID)
	log.Printf("\nPress Ctrl+C to stop")

	// Wait for interrupt
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Timeout after 2 minutes
	timeout := time.After(2 * time.Minute)

	select {
	case <-stop:
		log.Printf("\n👋 Stopping Discord test...")
	case <-timeout:
		log.Printf("\n⏰ Test timeout reached (2 minutes)")
		log.Printf("If the bot sent the initial message but didn't respond to !test:")
		log.Printf("  - Check that you typed !test in the correct channel")
		log.Printf("  - Verify your user ID is correct")
		log.Printf("  - Make sure the bot has 'Read Message History' permission")
	}

	log.Printf("Discord test complete!")
}
