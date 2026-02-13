package jobs

import (
	"encoding/json"
	"time"

	"strings"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// Task types
const (
	TypeMediaProcess       = "media:process"
	TypeCleanupFiles       = "files:cleanup"
	TypeCleanupStaleJobs   = "jobs:cleanup"
	TypeCleanupAnonProfiles = "profiles:cleanup_anon"
)

// QueueClient handles job queue operations
type QueueClient struct {
	client *asynq.Client
	logger *zap.Logger
}

// NewQueueClient creates a new queue client
func NewQueueClient(redisAddr string, logger *zap.Logger) *QueueClient {
	var opts asynq.RedisConnOpt
	var err error

	if strings.HasPrefix(redisAddr, "redis://") || strings.HasPrefix(redisAddr, "rediss://") {
		opts, err = asynq.ParseRedisURI(redisAddr)
		if err != nil {
			logger.Fatal("Failed to parse Redis URI", zap.Error(err))
		}
	} else {
		opts = asynq.RedisClientOpt{Addr: redisAddr}
	}

	client := asynq.NewClient(opts)
	return &QueueClient{
		client: client,
		logger: logger,
	}
}

// Close closes the queue client
func (q *QueueClient) Close() error {
	return q.client.Close()
}

// MediaProcessPayload contains media processing task data
type MediaProcessPayload struct {
	JobID      string      `json:"jobId"`
	InputPath  string      `json:"inputPath"`
	InputPaths []string    `json:"inputPaths,omitempty"` // For merge operations
	OutputPath string      `json:"outputPath"`
	Operations []Operation `json:"operations"`
	UseGPU     bool        `json:"useGpu,omitempty"` // Pro tier: enable hardware acceleration
}

// CleanupPayload contains file cleanup task data
type CleanupPayload struct {
	Zone      string `json:"zone"`
	OlderThan int64  `json:"olderThan"` // Unix timestamp
}

// StaleJobCleanupPayload contains stale job cleanup task data
type StaleJobCleanupPayload struct {
	AnonMaxAgeDays int `json:"anonMaxAgeDays"` // Max age in days for anonymous user jobs (default 7)
	AuthMaxAgeDays int `json:"authMaxAgeDays"` // Max age in days for authenticated user jobs (default 30)
}

// AnonProfileCleanupPayload contains anonymous profile pruning task data
type AnonProfileCleanupPayload struct {
	InactiveDays int `json:"inactiveDays"` // Delete anon profiles inactive for this many days (default 60)
}

// EnqueueMediaProcess queues a media processing task
func (q *QueueClient) EnqueueMediaProcess(payload MediaProcessPayload, priority string) (*asynq.TaskInfo, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	task := asynq.NewTask(TypeMediaProcess, data)

	opts := []asynq.Option{
		asynq.MaxRetry(3),
		asynq.Timeout(2 * time.Hour),
	}

	switch priority {
	case "high":
		opts = append(opts, asynq.Queue("critical"))
	case "low":
		opts = append(opts, asynq.Queue("low"))
	default:
		opts = append(opts, asynq.Queue("default"))
	}

	info, err := q.client.Enqueue(task, opts...)
	if err != nil {
		q.logger.Error("Failed to enqueue media process task", zap.Error(err))
		return nil, err
	}

	q.logger.Info("Media process task enqueued",
		zap.String("task_id", info.ID),
		zap.String("job_id", payload.JobID),
	)

	return info, nil
}

// EnqueueCleanup queues a file cleanup task
func (q *QueueClient) EnqueueCleanup(payload CleanupPayload) (*asynq.TaskInfo, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	task := asynq.NewTask(TypeCleanupFiles, data)

	opts := []asynq.Option{
		asynq.MaxRetry(1),
		asynq.Queue("low"),
	}

	return q.client.Enqueue(task, opts...)
}

// ScheduleCleanup schedules periodic cleanup tasks:
// - Hourly: permanently deletes files past 24h expiry
// - Daily: removes stale completed/failed jobs and inactive anonymous profiles
func (q *QueueClient) ScheduleCleanup(redisAddr string) (*asynq.Scheduler, error) {
	var opts asynq.RedisConnOpt
	var err error

	if strings.HasPrefix(redisAddr, "redis://") || strings.HasPrefix(redisAddr, "rediss://") {
		opts, err = asynq.ParseRedisURI(redisAddr)
		if err != nil {
			return nil, err
		}
	} else {
		opts = asynq.RedisClientOpt{Addr: redisAddr}
	}

	scheduler := asynq.NewScheduler(
		opts,
		&asynq.SchedulerOpts{},
	)

	// Hourly: file cleanup
	filePayload, _ := json.Marshal(CleanupPayload{Zone: "all"})
	if _, err := scheduler.Register("@hourly", asynq.NewTask(TypeCleanupFiles, filePayload)); err != nil {
		return nil, err
	}

	// Daily at 3 AM: stale job cleanup
	jobPayload, _ := json.Marshal(StaleJobCleanupPayload{AnonMaxAgeDays: 7, AuthMaxAgeDays: 30})
	if _, err := scheduler.Register("0 3 * * *", asynq.NewTask(TypeCleanupStaleJobs, jobPayload)); err != nil {
		return nil, err
	}

	// Daily at 4 AM: anonymous profile pruning
	profilePayload, _ := json.Marshal(AnonProfileCleanupPayload{InactiveDays: 60})
	if _, err := scheduler.Register("0 4 * * *", asynq.NewTask(TypeCleanupAnonProfiles, profilePayload)); err != nil {
		return nil, err
	}

	return scheduler, nil
}
