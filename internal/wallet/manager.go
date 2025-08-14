package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"prop-voter/config"
	"prop-voter/internal/models"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Manager handles secure wallet storage and management
type Manager struct {
	db     *gorm.DB
	config *config.Config
	logger *zap.Logger
	gcm    cipher.AEAD
}

// NewManager creates a new wallet manager
func NewManager(db *gorm.DB, config *config.Config, logger *zap.Logger) (*Manager, error) {
	// Create AES cipher for encryption
	key := sha256.Sum256([]byte(config.Security.EncryptionKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Manager{
		db:     db,
		config: config,
		logger: logger,
		gcm:    gcm,
	}, nil
}

// StoreWallet securely stores wallet information for a chain
func (m *Manager) StoreWallet(chainID, keyName, address, privateData string) error {
	// Encrypt the private data
	encryptedData, err := m.encrypt(privateData)
	if err != nil {
		return fmt.Errorf("failed to encrypt wallet data: %w", err)
	}

	wallet := models.WalletInfo{
		ChainID:      chainID,
		KeyName:      keyName,
		Address:      address,
		EncryptedKey: encryptedData,
	}

	// Check if wallet already exists
	var existing models.WalletInfo
	result := m.db.Where("chain_id = ?", chainID).First(&existing)
	
	if result.Error == gorm.ErrRecordNotFound {
		// Create new wallet
		if err := m.db.Create(&wallet).Error; err != nil {
			return fmt.Errorf("failed to store wallet: %w", err)
		}
		
		m.logger.Info("Wallet stored successfully",
			zap.String("chain_id", chainID),
			zap.String("address", address),
		)
	} else if result.Error == nil {
		// Update existing wallet
		existing.KeyName = keyName
		existing.Address = address
		existing.EncryptedKey = encryptedData
		
		if err := m.db.Save(&existing).Error; err != nil {
			return fmt.Errorf("failed to update wallet: %w", err)
		}
		
		m.logger.Info("Wallet updated successfully",
			zap.String("chain_id", chainID),
			zap.String("address", address),
		)
	} else {
		return fmt.Errorf("database error: %w", result.Error)
	}

	return nil
}

// GetWallet retrieves and decrypts wallet information for a chain
func (m *Manager) GetWallet(chainID string) (*models.WalletInfo, string, error) {
	var wallet models.WalletInfo
	if err := m.db.Where("chain_id = ?", chainID).First(&wallet).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", fmt.Errorf("wallet not found for chain %s", chainID)
		}
		return nil, "", fmt.Errorf("database error: %w", err)
	}

	// Decrypt the private data
	decryptedData, err := m.decrypt(wallet.EncryptedKey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decrypt wallet data: %w", err)
	}

	return &wallet, decryptedData, nil
}

// ListWallets returns all stored wallet addresses (without private data)
func (m *Manager) ListWallets() ([]models.WalletInfo, error) {
	var wallets []models.WalletInfo
	if err := m.db.Find(&wallets).Error; err != nil {
		return nil, fmt.Errorf("failed to list wallets: %w", err)
	}

	// Clear encrypted data from response for security
	for i := range wallets {
		wallets[i].EncryptedKey = ""
	}

	return wallets, nil
}

// DeleteWallet removes wallet information for a chain
func (m *Manager) DeleteWallet(chainID string) error {
	result := m.db.Where("chain_id = ?", chainID).Delete(&models.WalletInfo{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete wallet: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("wallet not found for chain %s", chainID)
	}

	m.logger.Info("Wallet deleted successfully", zap.String("chain_id", chainID))
	return nil
}

// ValidateWalletExists checks if a wallet exists for a chain
func (m *Manager) ValidateWalletExists(chainID string) error {
	var count int64
	if err := m.db.Model(&models.WalletInfo{}).Where("chain_id = ?", chainID).Count(&count).Error; err != nil {
		return fmt.Errorf("database error: %w", err)
	}

	if count == 0 {
		return fmt.Errorf("no wallet found for chain %s", chainID)
	}

	return nil
}

// encrypt encrypts data using AES-GCM
func (m *Manager) encrypt(data string) (string, error) {
	nonce := make([]byte, m.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := m.gcm.Seal(nonce, nonce, []byte(data), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts data using AES-GCM
func (m *Manager) decrypt(encryptedData string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedData)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	nonceSize := m.gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := m.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// ExportWallet exports wallet in a secure format (for backup purposes)
func (m *Manager) ExportWallet(chainID string) (map[string]interface{}, error) {
	wallet, privateData, err := m.GetWallet(chainID)
	if err != nil {
		return nil, err
	}

	export := map[string]interface{}{
		"chain_id":    wallet.ChainID,
		"key_name":    wallet.KeyName,
		"address":     wallet.Address,
		"private_data": privateData, // This should be handled carefully
		"created_at":  wallet.CreatedAt,
	}

	m.logger.Warn("Wallet exported - handle with extreme care",
		zap.String("chain_id", chainID),
	)

	return export, nil
}