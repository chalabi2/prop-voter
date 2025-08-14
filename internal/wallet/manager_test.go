package wallet

import (
	"testing"

	"prop-voter/config"
	"prop-voter/internal/models"

	"go.uber.org/zap/zaptest"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestManager(t testing.TB) (*Manager, *gorm.DB) {
	// Create in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Initialize tables
	if err := models.InitDB(db); err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	cfg := &config.Config{
		Security: config.SecurityConfig{
			EncryptionKey: "test-encryption-key-32-characters",
		},
	}

	logger := zaptest.NewLogger(t)

	manager, err := NewManager(db, cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create wallet manager: %v", err)
	}

	return manager, db
}

func TestNewManager(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cfg := &config.Config{
		Security: config.SecurityConfig{
			EncryptionKey: "test-encryption-key-32-characters",
		},
	}

	logger := zaptest.NewLogger(t)

	manager, err := NewManager(db, cfg, logger)
	if err != nil {
		t.Fatalf("Failed to create wallet manager: %v", err)
	}

	if manager.db != db {
		t.Error("Expected manager database to match provided database")
	}

	if manager.config != cfg {
		t.Error("Expected manager config to match provided config")
	}

	if manager.logger != logger {
		t.Error("Expected manager logger to match provided logger")
	}

	if manager.gcm == nil {
		t.Error("Expected GCM cipher to be initialized")
	}
}

func TestNewManagerInvalidKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cfg := &config.Config{
		Security: config.SecurityConfig{
			EncryptionKey: "short", // Too short for AES (needs to be padded to 32 bytes by SHA256)
		},
	}

	logger := zaptest.NewLogger(t)

	// Actually, the NewManager function uses SHA256 hash of the key, so even short keys work
	// Let's test with a genuinely problematic scenario or skip this test
	manager, err := NewManager(db, cfg, logger)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if manager == nil {
		t.Error("Expected manager to be created even with short key (SHA256 handles it)")
	}
}

func TestStoreWallet(t *testing.T) {
	manager, _ := setupTestManager(t)

	err := manager.StoreWallet("test-chain-1", "test-key", "test1abc123", "private-data-here")
	if err != nil {
		t.Fatalf("Failed to store wallet: %v", err)
	}

	// Verify wallet was stored
	var wallet models.WalletInfo
	err = manager.db.Where("chain_id = ?", "test-chain-1").First(&wallet).Error
	if err != nil {
		t.Fatalf("Failed to retrieve stored wallet: %v", err)
	}

	if wallet.ChainID != "test-chain-1" {
		t.Errorf("Expected chain ID 'test-chain-1', got '%s'", wallet.ChainID)
	}

	if wallet.KeyName != "test-key" {
		t.Errorf("Expected key name 'test-key', got '%s'", wallet.KeyName)
	}

	if wallet.Address != "test1abc123" {
		t.Errorf("Expected address 'test1abc123', got '%s'", wallet.Address)
	}

	if wallet.EncryptedKey == "" {
		t.Error("Expected encrypted key to not be empty")
	}

	if wallet.EncryptedKey == "private-data-here" {
		t.Error("Expected encrypted key to be different from plaintext")
	}
}

func TestStoreWalletUpdate(t *testing.T) {
	manager, _ := setupTestManager(t)

	// Store initial wallet
	err := manager.StoreWallet("test-chain-1", "test-key", "test1abc123", "private-data-here")
	if err != nil {
		t.Fatalf("Failed to store initial wallet: %v", err)
	}

	// Update the same wallet
	err = manager.StoreWallet("test-chain-1", "updated-key", "test1xyz789", "updated-private-data")
	if err != nil {
		t.Fatalf("Failed to update wallet: %v", err)
	}

	// Verify there's still only one wallet for this chain
	var count int64
	manager.db.Model(&models.WalletInfo{}).Where("chain_id = ?", "test-chain-1").Count(&count)
	if count != 1 {
		t.Errorf("Expected 1 wallet for chain, got %d", count)
	}

	// Verify wallet was updated
	var wallet models.WalletInfo
	err = manager.db.Where("chain_id = ?", "test-chain-1").First(&wallet).Error
	if err != nil {
		t.Fatalf("Failed to retrieve updated wallet: %v", err)
	}

	if wallet.KeyName != "updated-key" {
		t.Errorf("Expected updated key name 'updated-key', got '%s'", wallet.KeyName)
	}

	if wallet.Address != "test1xyz789" {
		t.Errorf("Expected updated address 'test1xyz789', got '%s'", wallet.Address)
	}
}

func TestGetWallet(t *testing.T) {
	manager, _ := setupTestManager(t)

	originalData := "sensitive-private-key-data"

	// Store wallet
	err := manager.StoreWallet("test-chain-1", "test-key", "test1abc123", originalData)
	if err != nil {
		t.Fatalf("Failed to store wallet: %v", err)
	}

	// Retrieve wallet
	wallet, decryptedData, err := manager.GetWallet("test-chain-1")
	if err != nil {
		t.Fatalf("Failed to get wallet: %v", err)
	}

	if wallet.ChainID != "test-chain-1" {
		t.Errorf("Expected chain ID 'test-chain-1', got '%s'", wallet.ChainID)
	}

	if wallet.KeyName != "test-key" {
		t.Errorf("Expected key name 'test-key', got '%s'", wallet.KeyName)
	}

	if wallet.Address != "test1abc123" {
		t.Errorf("Expected address 'test1abc123', got '%s'", wallet.Address)
	}

	if decryptedData != originalData {
		t.Errorf("Expected decrypted data '%s', got '%s'", originalData, decryptedData)
	}
}

func TestGetWalletNotFound(t *testing.T) {
	manager, _ := setupTestManager(t)

	_, _, err := manager.GetWallet("non-existent-chain")
	if err == nil {
		t.Error("Expected error for non-existent wallet")
	}

	expectedError := "wallet not found for chain non-existent-chain"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestListWallets(t *testing.T) {
	manager, _ := setupTestManager(t)

	// Store multiple wallets
	wallets := []struct {
		chainID string
		keyName string
		address string
	}{
		{"chain-1", "key-1", "addr-1"},
		{"chain-2", "key-2", "addr-2"},
		{"chain-3", "key-3", "addr-3"},
	}

	for _, w := range wallets {
		err := manager.StoreWallet(w.chainID, w.keyName, w.address, "private-data")
		if err != nil {
			t.Fatalf("Failed to store wallet %s: %v", w.chainID, err)
		}
	}

	// List wallets
	retrieved, err := manager.ListWallets()
	if err != nil {
		t.Fatalf("Failed to list wallets: %v", err)
	}

	if len(retrieved) != len(wallets) {
		t.Errorf("Expected %d wallets, got %d", len(wallets), len(retrieved))
	}

	// Verify encrypted keys are cleared
	for _, wallet := range retrieved {
		if wallet.EncryptedKey != "" {
			t.Error("Expected encrypted key to be cleared in list response")
		}
	}

	// Verify wallet data
	chainIDsFound := make(map[string]bool)
	for _, wallet := range retrieved {
		chainIDsFound[wallet.ChainID] = true
	}

	for _, w := range wallets {
		if !chainIDsFound[w.chainID] {
			t.Errorf("Expected to find wallet for chain %s", w.chainID)
		}
	}
}

func TestDeleteWallet(t *testing.T) {
	manager, _ := setupTestManager(t)

	// Store wallet
	err := manager.StoreWallet("test-chain-1", "test-key", "test1abc123", "private-data")
	if err != nil {
		t.Fatalf("Failed to store wallet: %v", err)
	}

	// Delete wallet
	err = manager.DeleteWallet("test-chain-1")
	if err != nil {
		t.Fatalf("Failed to delete wallet: %v", err)
	}

	// Verify wallet was deleted
	var count int64
	manager.db.Model(&models.WalletInfo{}).Where("chain_id = ?", "test-chain-1").Count(&count)
	if count != 0 {
		t.Errorf("Expected 0 wallets after deletion, got %d", count)
	}
}

func TestDeleteWalletNotFound(t *testing.T) {
	manager, _ := setupTestManager(t)

	err := manager.DeleteWallet("non-existent-chain")
	if err == nil {
		t.Error("Expected error when deleting non-existent wallet")
	}

	expectedError := "wallet not found for chain non-existent-chain"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}

func TestValidateWalletExists(t *testing.T) {
	manager, _ := setupTestManager(t)

	// Test non-existent wallet
	err := manager.ValidateWalletExists("non-existent-chain")
	if err == nil {
		t.Error("Expected error for non-existent wallet")
	}

	// Store wallet
	err = manager.StoreWallet("test-chain-1", "test-key", "test1abc123", "private-data")
	if err != nil {
		t.Fatalf("Failed to store wallet: %v", err)
	}

	// Test existing wallet
	err = manager.ValidateWalletExists("test-chain-1")
	if err != nil {
		t.Errorf("Expected no error for existing wallet, got: %v", err)
	}
}

func TestExportWallet(t *testing.T) {
	manager, _ := setupTestManager(t)

	originalData := "sensitive-private-key-data"

	// Store wallet
	err := manager.StoreWallet("test-chain-1", "test-key", "test1abc123", originalData)
	if err != nil {
		t.Fatalf("Failed to store wallet: %v", err)
	}

	// Export wallet
	exported, err := manager.ExportWallet("test-chain-1")
	if err != nil {
		t.Fatalf("Failed to export wallet: %v", err)
	}

	if exported["chain_id"] != "test-chain-1" {
		t.Errorf("Expected chain_id 'test-chain-1', got '%v'", exported["chain_id"])
	}

	if exported["key_name"] != "test-key" {
		t.Errorf("Expected key_name 'test-key', got '%v'", exported["key_name"])
	}

	if exported["address"] != "test1abc123" {
		t.Errorf("Expected address 'test1abc123', got '%v'", exported["address"])
	}

	if exported["private_data"] != originalData {
		t.Errorf("Expected private_data '%s', got '%v'", originalData, exported["private_data"])
	}

	if exported["created_at"] == nil {
		t.Error("Expected created_at to be present")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	manager, _ := setupTestManager(t)

	originalData := "this-is-sensitive-data-to-encrypt"

	// Test encryption
	encrypted, err := manager.encrypt(originalData)
	if err != nil {
		t.Fatalf("Failed to encrypt data: %v", err)
	}

	if encrypted == originalData {
		t.Error("Expected encrypted data to be different from original")
	}

	if encrypted == "" {
		t.Error("Expected encrypted data to not be empty")
	}

	// Test decryption
	decrypted, err := manager.decrypt(encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt data: %v", err)
	}

	if decrypted != originalData {
		t.Errorf("Expected decrypted data '%s', got '%s'", originalData, decrypted)
	}
}

func TestDecryptInvalidData(t *testing.T) {
	manager, _ := setupTestManager(t)

	// Test with invalid base64
	_, err := manager.decrypt("invalid-base64!")
	if err == nil {
		t.Error("Expected error with invalid base64")
	}

	// Test with too short data
	_, err = manager.decrypt("dGVzdA==") // "test" in base64, too short for GCM
	if err == nil {
		t.Error("Expected error with too short ciphertext")
	}

	// Test with wrong key (create another manager with different key)
	cfg2 := &config.Config{
		Security: config.SecurityConfig{
			EncryptionKey: "different-key-32-characters!!",
		},
	}
	logger := zaptest.NewLogger(t)
	manager2, err := NewManager(manager.db, cfg2, logger)
	if err != nil {
		t.Fatalf("Failed to create second manager: %v", err)
	}

	// Encrypt with first manager
	encrypted, err := manager.encrypt("test-data")
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	// Try to decrypt with second manager (different key)
	_, err = manager2.decrypt(encrypted)
	if err == nil {
		t.Error("Expected error when decrypting with wrong key")
	}
}

// Benchmark tests
func BenchmarkStoreWallet(b *testing.B) {
	manager, _ := setupTestManager(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chainID := "test-chain-" + string(rune(i))
		err := manager.StoreWallet(chainID, "test-key", "test-address", "private-data")
		if err != nil {
			b.Fatalf("Failed to store wallet: %v", err)
		}
	}
}

func BenchmarkGetWallet(b *testing.B) {
	manager, _ := setupTestManager(b)

	// Store a wallet
	err := manager.StoreWallet("test-chain", "test-key", "test-address", "private-data")
	if err != nil {
		b.Fatalf("Failed to store wallet: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := manager.GetWallet("test-chain")
		if err != nil {
			b.Fatalf("Failed to get wallet: %v", err)
		}
	}
}

func BenchmarkEncryptDecrypt(b *testing.B) {
	manager, _ := setupTestManager(b)
	data := "this-is-some-sensitive-data-to-encrypt-and-decrypt"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encrypted, err := manager.encrypt(data)
		if err != nil {
			b.Fatalf("Failed to encrypt: %v", err)
		}

		_, err = manager.decrypt(encrypted)
		if err != nil {
			b.Fatalf("Failed to decrypt: %v", err)
		}
	}
}
