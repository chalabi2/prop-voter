package models

import (
	"time"

	"gorm.io/gorm"
)

// Proposal represents a governance proposal from any chain
type Proposal struct {
	ID          uint   `gorm:"primaryKey"`
	ChainID     string `gorm:"index;not null"`
	ProposalID  string `gorm:"index;not null"`
	Title       string
	Description string
	Status      string
	VotingStart *time.Time
	VotingEnd   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time

	// Notification tracking
	NotificationSent bool `gorm:"default:false"`

	// Voting tracking
	Vote *Vote `gorm:"foreignKey:ProposalID,ChainID;references:ProposalID,ChainID"`
}

// Vote represents a vote cast on a proposal
type Vote struct {
	ID         uint   `gorm:"primaryKey"`
	ChainID    string `gorm:"index;not null"`
	ProposalID string `gorm:"index;not null"`
	Option     string // yes, no, abstain, no_with_veto
	TxHash     string
	VotedAt    time.Time
	CreatedAt  time.Time
}

// WalletInfo stores encrypted wallet information
type WalletInfo struct {
	ID            uint   `gorm:"primaryKey"`
	ChainID       string `gorm:"uniqueIndex;not null"`
	KeyName       string
	Address       string
	EncryptedKey  string // Encrypted private key or mnemonic
	KeyDerivation string // Key derivation path if needed
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// NotificationLog tracks sent notifications to avoid duplicates
type NotificationLog struct {
	ID         uint   `gorm:"primaryKey"`
	ChainID    string `gorm:"index;not null"`
	ProposalID string `gorm:"index;not null"`
	Type       string // "new_proposal", "voting_started", "voting_ending"
	SentAt     time.Time
}

// InitDB initializes the database and creates tables
func InitDB(db *gorm.DB) error {
	return db.AutoMigrate(
		&Proposal{},
		&Vote{},
		&WalletInfo{},
		&NotificationLog{},
	)
}