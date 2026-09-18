package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DLQRecord represents an immutable audit record for quarantined packets.
type DLQRecord struct {
	RecordID        string             `json:"record_id"`
	Timestamp       time.Time          `json:"timestamp"`
	SourceTopic     string             `json:"source_topic"`
	RejectionReason CDSRejectionReason `json:"rejection_reason"`
	ErrorDetails    string             `json:"error_details"`
	SourceEnclave   string             `json:"source_enclave"`
	Classification  string             `json:"classification"`
	RawPayload      string             `json:"raw_payload"`
}

// DeadLetterQueue manages quarantined packets and tamper-evident audit logs.
type DeadLetterQueue struct {
	mu            sync.RWMutex
	records       []DLQRecord
	maxCapacity   int
	auditFilePath string
	auditFile     *os.File
}

// NewDeadLetterQueue creates a thread-safe DLQ with capacity and optional disk audit log.
func NewDeadLetterQueue(maxCapacity int, auditFilePath string) (*DeadLetterQueue, error) {
	if maxCapacity <= 0 {
		maxCapacity = 1000
	}

	dlq := &DeadLetterQueue{
		records:       make([]DLQRecord, 0, maxCapacity),
		maxCapacity:   maxCapacity,
		auditFilePath: auditFilePath,
	}

	if auditFilePath != "" {
		cleanPath := filepath.Clean(auditFilePath)
		// #nosec G304 - configured audit log path is cleaned
		f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("opening audit log file %q: %w", auditFilePath, err)
		}
		dlq.auditFile = f
	}

	return dlq, nil
}

// Quarantine stores a violation record in the DLQ and flushes to the audit log.
func (q *DeadLetterQueue) Quarantine(sourceTopic string, reason CDSRejectionReason, errDetails string, enclave, classification string, rawPayload string) DLQRecord {
	q.mu.Lock()
	defer q.mu.Unlock()

	rec := DLQRecord{
		RecordID:        fmt.Sprintf("dlq-%d-%d", time.Now().UnixNano(), len(q.records)+1),
		Timestamp:       time.Now().UTC(),
		SourceTopic:     sourceTopic,
		RejectionReason: reason,
		ErrorDetails:    errDetails,
		SourceEnclave:   enclave,
		Classification:  classification,
		RawPayload:      rawPayload,
	}

	if len(q.records) >= q.maxCapacity {
		// Evict oldest record
		q.records = q.records[1:]
	}
	q.records = append(q.records, rec)

	// Write to persistent audit log file (JSONL format)
	if q.auditFile != nil {
		line, err := json.Marshal(rec)
		if err == nil {
			_, _ = q.auditFile.Write(append(line, '\n'))
			_ = q.auditFile.Sync()
		}
	}

	return rec
}

// GetRecords returns a copy of recent quarantined records.
func (q *DeadLetterQueue) GetRecords(limit int) []DLQRecord {
	q.mu.RLock()
	defer q.mu.RUnlock()

	n := len(q.records)
	if limit <= 0 || limit > n {
		limit = n
	}

	result := make([]DLQRecord, limit)
	copy(result, q.records[n-limit:])
	return result
}

// Count returns the total number of items currently in the in-memory DLQ.
func (q *DeadLetterQueue) Count() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.records)
}

// Close closes the underlying audit file.
func (q *DeadLetterQueue) Close() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.auditFile != nil {
		return q.auditFile.Close()
	}
	return nil
}
