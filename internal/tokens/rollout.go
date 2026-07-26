package tokens

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
)

const maxTailBytes int64 = 1 << 20

type Snapshot struct {
	RolloutPath  string
	Offset       int64
	Size         int64
	OutputTokens uint64
	TotalTokens  uint64
	Found        bool
}

func ReadRollout(path string, previous Snapshot) (Snapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Snapshot{}, err
	}
	size := info.Size()
	if previous.RolloutPath == path && previous.Size == size {
		return previous, nil
	}

	start := max(int64(0), size-maxTailBytes)
	incremental := previous.RolloutPath == path && previous.Offset > 0 && previous.Size < size && previous.Offset < size && size-previous.Offset <= maxTailBytes
	if incremental {
		start = previous.Offset
	}
	file, err := os.Open(path)
	if err != nil {
		return Snapshot{}, err
	}
	defer file.Close()
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return Snapshot{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxTailBytes))
	if err != nil {
		return Snapshot{}, err
	}
	dataStart := start
	if start > 0 && !incremental {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			dataStart += int64(newline + 1)
			data = data[newline+1:]
		} else {
			data = nil
			dataStart = size
		}
	}

	snapshot := Snapshot{RolloutPath: path, Offset: size, Size: size}
	if incremental {
		snapshot.OutputTokens = previous.OutputTokens
		snapshot.TotalTokens = previous.TotalTokens
		snapshot.Found = previous.Found
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lineStart := bytes.LastIndexByte(data, '\n') + 1
		line := bytes.TrimSpace(data[lineStart:])
		if len(line) > 0 && !json.Valid(line) {
			snapshot.Offset = dataStart + int64(lineStart)
			snapshot.OutputTokens = 0
			snapshot.TotalTokens = 0
			snapshot.Found = false
			return snapshot, nil
		}
	}
	switch usage, status := lastUsage(data); status {
	case usageFound:
		snapshot.OutputTokens = usage.OutputTokens
		snapshot.TotalTokens = usage.TotalTokens
		snapshot.Found = true
	case usageInvalid:
		snapshot.OutputTokens = 0
		snapshot.TotalTokens = 0
		snapshot.Found = false
	}
	return snapshot, nil
}

type usage struct {
	OutputTokens uint64
	TotalTokens  uint64
}

type usageStatus uint8

const (
	usageMissing usageStatus = iota
	usageFound
	usageInvalid
)

func lastUsage(data []byte) (usage, usageStatus) {
	for end := len(data); end > 0; {
		start := bytes.LastIndexByte(data[:end], '\n') + 1
		line := bytes.TrimSpace(data[start:end])
		if len(line) > 0 {
			var event struct {
				Type    string `json:"type"`
				Payload struct {
					Type string `json:"type"`
					Info struct {
						TotalTokenUsage *struct {
							OutputTokens *uint64 `json:"output_tokens"`
							TotalTokens  uint64  `json:"total_tokens"`
						} `json:"total_token_usage"`
					} `json:"info"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(line, &event); err != nil {
				return usage{}, usageInvalid
			}
			if event.Type == "event_msg" && event.Payload.Type == "token_count" {
				if event.Payload.Info.TotalTokenUsage == nil || event.Payload.Info.TotalTokenUsage.OutputTokens == nil {
					return usage{}, usageInvalid
				}
				return usage{
					OutputTokens: *event.Payload.Info.TotalTokenUsage.OutputTokens,
					TotalTokens:  event.Payload.Info.TotalTokenUsage.TotalTokens,
				}, usageFound
			}
		}
		if start == 0 {
			break
		}
		end = start - 1
	}
	return usage{}, usageMissing
}

func Format(value uint64) string {
	if value < 1000 {
		return strconv.FormatUint(value, 10)
	}
	units := []string{"k", "m", "b", "t"}
	scaled := float64(value)
	unit := ""
	for _, candidate := range units {
		scaled /= 1000
		unit = candidate
		if scaled < 1000 {
			break
		}
	}
	scaled = roundSignificant(scaled, 2)
	if scaled >= 1000 && unit != units[len(units)-1] {
		for index, candidate := range units {
			if candidate == unit {
				scaled = roundSignificant(scaled/1000, 2)
				unit = units[index+1]
				break
			}
		}
	}
	decimals := 0
	if scaled < 10 {
		decimals = 1
	}
	number := strconv.FormatFloat(scaled, 'f', decimals, 64)
	number = strings.TrimSuffix(number, ".0")
	return number + unit
}

func roundSignificant(value float64, digits int) float64 {
	if value == 0 {
		return 0
	}
	scale := math.Pow10(int(math.Floor(math.Log10(math.Abs(value)))) - digits + 1)
	return math.Round(value/scale) * scale
}
