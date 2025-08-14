package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"prop-voter/config"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Server represents the health check server
type Server struct {
	config     *config.Config
	db         *gorm.DB
	logger     *zap.Logger
	server     *http.Server
	startTime  time.Time
	lastScan   time.Time
	scanErrors int64
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status      string            `json:"status"`
	Timestamp   time.Time         `json:"timestamp"`
	Uptime      string            `json:"uptime"`
	Version     string            `json:"version,omitempty"`
	Services    map[string]string `json:"services"`
	Metrics     HealthMetrics     `json:"metrics"`
	LastScan    *time.Time        `json:"last_scan,omitempty"`
	Environment map[string]string `json:"environment"`
}

// HealthMetrics represents system metrics
type HealthMetrics struct {
	GoRoutines   int   `json:"goroutines"`
	MemoryMB     int   `json:"memory_mb"`
	ScanErrors   int64 `json:"scan_errors"`
	TotalChains  int   `json:"total_chains"`
	ActiveChains int   `json:"active_chains"`
}

// NewServer creates a new health check server
func NewServer(config *config.Config, db *gorm.DB, logger *zap.Logger) *Server {
	return &Server{
		config:    config,
		db:        db,
		logger:    logger,
		startTime: time.Now(),
	}
}

// Start starts the health check server
func (s *Server) Start(ctx context.Context) error {
	if !s.config.Health.Enabled {
		s.logger.Info("Health check server disabled")
		return nil
	}

	mux := http.NewServeMux()
	mux.HandleFunc(s.config.Health.Path, s.healthHandler)
	mux.HandleFunc("/metrics", s.metricsHandler)
	mux.HandleFunc("/ready", s.readinessHandler)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.config.Health.Port),
		Handler: mux,
	}

	s.logger.Info("Starting health check server",
		zap.Int("port", s.config.Health.Port),
		zap.String("path", s.config.Health.Path),
	)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Health server error", zap.Error(err))
		}
	}()

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		s.logger.Info("Shutting down health server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Health server shutdown error", zap.Error(err))
		}
	}()

	return nil
}

// healthHandler handles the main health check endpoint
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := "healthy"
	statusCode := http.StatusOK

	// Check database connection
	services := make(map[string]string)
	sqlDB, err := s.db.DB()
	if err != nil {
		services["database"] = "error: " + err.Error()
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	} else {
		if err := sqlDB.Ping(); err != nil {
			services["database"] = "error: " + err.Error()
			status = "unhealthy"
			statusCode = http.StatusServiceUnavailable
		} else {
			services["database"] = "healthy"
		}
	}

	// Check Discord connection (basic)
	if s.config.Discord.Token == "" {
		services["discord"] = "not configured"
		status = "degraded"
		if statusCode == http.StatusOK {
			statusCode = http.StatusPartialContent
		}
	} else {
		services["discord"] = "configured"
	}

	// Check chains configuration
	activeChains := 0
	for _, chain := range s.config.Chains {
		if chain.RPC != "" && chain.REST != "" {
			activeChains++
		}
	}

	if activeChains == 0 {
		services["chains"] = "no active chains"
		status = "degraded"
		if statusCode == http.StatusOK {
			statusCode = http.StatusPartialContent
		}
	} else {
		services["chains"] = fmt.Sprintf("%d configured", activeChains)
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	response := HealthResponse{
		Status:    status,
		Timestamp: time.Now(),
		Uptime:    time.Since(s.startTime).String(),
		Services:  services,
		Metrics: HealthMetrics{
			GoRoutines:   runtime.NumGoroutine(),
			MemoryMB:     int(m.Alloc / 1024 / 1024),
			ScanErrors:   s.scanErrors,
			TotalChains:  len(s.config.Chains),
			ActiveChains: activeChains,
		},
		Environment: map[string]string{
			"go_version": runtime.Version(),
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
		},
	}

	if !s.lastScan.IsZero() {
		response.LastScan = &s.lastScan
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// metricsHandler provides Prometheus-style metrics
func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	uptime := time.Since(s.startTime).Seconds()

	metrics := fmt.Sprintf(`# HELP prop_voter_uptime_seconds Total uptime in seconds
# TYPE prop_voter_uptime_seconds counter
prop_voter_uptime_seconds %f

# HELP prop_voter_goroutines Number of goroutines
# TYPE prop_voter_goroutines gauge
prop_voter_goroutines %d

# HELP prop_voter_memory_bytes Memory usage in bytes
# TYPE prop_voter_memory_bytes gauge
prop_voter_memory_bytes %d

# HELP prop_voter_scan_errors_total Total number of scan errors
# TYPE prop_voter_scan_errors_total counter
prop_voter_scan_errors_total %d

# HELP prop_voter_chains_configured Number of configured chains
# TYPE prop_voter_chains_configured gauge
prop_voter_chains_configured %d
`,
		uptime,
		runtime.NumGoroutine(),
		m.Alloc,
		s.scanErrors,
		len(s.config.Chains),
	)

	w.Write([]byte(metrics))
}

// readinessHandler checks if the service is ready to serve traffic
func (s *Server) readinessHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check if essential services are ready
	ready := true
	checks := make(map[string]bool)

	// Database ready
	sqlDB, err := s.db.DB()
	if err != nil || sqlDB.Ping() != nil {
		checks["database"] = false
		ready = false
	} else {
		checks["database"] = true
	}

	// Configuration ready
	if len(s.config.Chains) == 0 {
		checks["configuration"] = false
		ready = false
	} else {
		checks["configuration"] = true
	}

	response := map[string]interface{}{
		"ready":     ready,
		"checks":    checks,
		"timestamp": time.Now(),
	}

	if ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(response)
}

// UpdateScanMetrics updates scanning metrics
func (s *Server) UpdateScanMetrics(lastScan time.Time, errorCount int64) {
	s.lastScan = lastScan
	s.scanErrors = errorCount
}

// IncrementScanErrors increments the scan error counter
func (s *Server) IncrementScanErrors() {
	s.scanErrors++
}
