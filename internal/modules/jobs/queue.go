package jobs

import (
	"encoding/json"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

// Task types
const (
	TypeMediaProcess = "media:process"
	TypeCleanupFiles = "files:cleanup"
)

// QueueClient handles job queue operations
type QueueClient struct {
	client *asynq.Client
	logger *zap.Logger
}

// NewQueueClient creates a new queue client
func NewQueueClient(redisAddr string, logger *zap.Logger) *QueueClient {
	client := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
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
	OutputPath string      `json:"outputPath"`
	Operations []Operation `json:"operations"`
}

// CleanupPayload contains file cleanup task data
type CleanupPayload struct {
	Zone      string `json:"zone"`
	OlderThan int64  `json:"olderThan"` // Unix timestamp
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

// ScheduleCleanup schedules periodic cleanup
func (q *QueueClient) ScheduleCleanup() error {
	scheduler := asynq.NewScheduler(
		asynq.RedisClientOpt{Addr: "localhost:6379"},
		nil,
	)

	// Schedule upload zone cleanup (24h old files)
	uploadPayload, _ := json.Marshal(CleanupPayload{
		Zone:      "upload",
		OlderThan: time.Now().Add(-24 * time.Hour).Unix(),
	})
	scheduler.Register("@hourly", asynq.NewTask(TypeCleanupFiles, uploadPayload))

	// Schedule working zone cleanup (4h old files)
	workingPayload, _ := json.Marshal(CleanupPayload{
		Zone:      "working",
		OlderThan: time.Now().Add(-4 * time.Hour).Unix(),
	})
	scheduler.Register("@every 30m", asynq.NewTask(TypeCleanupFiles, workingPayload))

	// Schedule output zone cleanup (7 days old files)
	outputPayload, _ := json.Marshal(CleanupPayload{
		Zone:      "output",
		OlderThan: time.Now().Add(-168 * time.Hour).Unix(),
	})
	scheduler.Register("@daily", asynq.NewTask(TypeCleanupFiles, outputPayload))

	return scheduler.Start()
}
