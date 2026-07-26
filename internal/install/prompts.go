package install

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/ericlitman/threadbear/internal/config"
	"github.com/ericlitman/threadbear/internal/tokens"
)

var ErrCancelled = errors.New("operation cancelled")

type Preferences struct {
	HeartbeatSeconds             int
	ArchiveEnabled               bool
	ArchiveAfterDays             int
	RenameEnabled                bool
	TokenDisplay                 tokens.Position
	AgentsEnabled                bool
	ClassifierModel              string
	ClassifierEffort             config.ClassifierEffort
	ClassifierContextBudgetBytes int
}

func DefaultPreferences() Preferences {
	defaults := config.Default("pending-control-task")
	return preferencesFromConfig(defaults)
}

func preferencesFromConfig(value config.Config) Preferences {
	return Preferences{
		HeartbeatSeconds: value.HeartbeatSeconds, ArchiveEnabled: value.ArchiveEnabled,
		ArchiveAfterDays: value.ArchiveAfterDays, RenameEnabled: value.RenameEnabled,
		TokenDisplay:  value.TokenDisplay,
		AgentsEnabled: value.AgentsEnabled, ClassifierModel: value.ClassifierModel,
		ClassifierEffort:             value.ClassifierEffort,
		ClassifierContextBudgetBytes: value.ClassifierContextBudgetBytes,
	}
}

func (p Preferences) config(controlTaskID, codexExecutable, codexSpawnPath string) config.Config {
	return config.Config{
		SchemaVersion: config.CurrentSchemaVersion, ControlTaskID: controlTaskID, CodexExecutable: codexExecutable, CodexSpawnPath: codexSpawnPath,
		HeartbeatSeconds: p.HeartbeatSeconds, ArchiveEnabled: p.ArchiveEnabled,
		ArchiveAfterDays: p.ArchiveAfterDays, RenameEnabled: p.RenameEnabled,
		TokenDisplay:  p.TokenDisplay,
		AgentsEnabled: p.AgentsEnabled, ClassifierModel: p.ClassifierModel,
		ClassifierEffort:             p.ClassifierEffort,
		ClassifierContextBudgetBytes: p.ClassifierContextBudgetBytes,
	}
}

func (p Preferences) Validate() error {
	return p.config("pending-control-task", "", "").Validate()
}

type Preview struct {
	Operation string
	Lines     []string
}

type Prompter interface {
	Collect(Preferences) (Preferences, error)
	ShowPreview(Preview) error
	Confirm() (bool, error)
}

type TTYPrompter struct {
	reader *bufio.Reader
	writer io.Writer
	closer io.Closer
}

func OpenTTYPrompter() (*TTYPrompter, error) {
	file, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/tty: %w", err)
	}
	return &TTYPrompter{reader: bufio.NewReader(file), writer: file, closer: file}, nil
}

func NewTTYPrompter(input io.Reader, output io.Writer) *TTYPrompter {
	return &TTYPrompter{reader: bufio.NewReader(input), writer: output}
}

func (p *TTYPrompter) Close() error {
	if p.closer == nil {
		return nil
	}
	return p.closer.Close()
}

func (p *TTYPrompter) Collect(value Preferences) (Preferences, error) {
	var err error
	if value.HeartbeatSeconds, err = p.askInt("Heartbeat interval in seconds", value.HeartbeatSeconds); err != nil {
		return Preferences{}, err
	}
	if value.ArchiveEnabled, err = p.askBool("Automatically archive completed inactive tasks", value.ArchiveEnabled); err != nil {
		return Preferences{}, err
	}
	if value.ArchiveAfterDays, err = p.askInt("Archive inactivity interval in days", value.ArchiveAfterDays); err != nil {
		return Preferences{}, err
	}
	if value.RenameEnabled, err = p.askBool("Automatically maintain status and next-action titles", value.RenameEnabled); err != nil {
		return Preferences{}, err
	}
	tokenDisplay, err := p.askString("Show output tokens in managed titles (off, start, end)", string(value.TokenDisplay))
	if err != nil {
		return Preferences{}, err
	}
	value.TokenDisplay = tokens.Position(tokenDisplay)
	if value.AgentsEnabled, err = p.askBool("Install managed AGENTS.md instructions", value.AgentsEnabled); err != nil {
		return Preferences{}, err
	}
	if value.ClassifierModel, err = p.askString("Classifier model", value.ClassifierModel); err != nil {
		return Preferences{}, err
	}
	effort, err := p.askString("Classifier effort (low, medium, high, xhigh)", string(value.ClassifierEffort))
	if err != nil {
		return Preferences{}, err
	}
	value.ClassifierEffort = config.ClassifierEffort(effort)
	if value.ClassifierContextBudgetBytes, err = p.askInt("Classifier context budget in bytes", value.ClassifierContextBudgetBytes); err != nil {
		return Preferences{}, err
	}
	if err := value.Validate(); err != nil {
		return Preferences{}, err
	}
	return value, nil
}

func (p *TTYPrompter) ShowPreview(preview Preview) error {
	if _, err := fmt.Fprintf(p.writer, "\nThreadBear %s preview\n", preview.Operation); err != nil {
		return err
	}
	for _, line := range preview.Lines {
		if _, err := fmt.Fprintf(p.writer, "- %s\n", line); err != nil {
			return err
		}
	}
	return nil
}

func (p *TTYPrompter) Choose(label string, current bool) (bool, error) {
	return p.askBool(label, current)
}

func (p *TTYPrompter) Confirm() (bool, error) {
	answer, err := p.askString("Apply exactly this preview? (yes/no)", "no")
	if err != nil {
		return false, err
	}
	switch strings.ToLower(answer) {
	case "yes", "y":
		return true, nil
	case "no", "n":
		return false, nil
	default:
		return false, errors.New("confirmation must be yes or no")
	}
}

func (p *TTYPrompter) askString(label, current string) (string, error) {
	if _, err := fmt.Fprintf(p.writer, "%s [%s]: ", label, current); err != nil {
		return "", err
	}
	line, err := p.reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return current, nil
	}
	return line, nil
}

func (p *TTYPrompter) askInt(label string, current int) (int, error) {
	raw, err := p.askString(label, strconv.Itoa(current))
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", label)
	}
	return value, nil
}

func (p *TTYPrompter) askBool(label string, current bool) (bool, error) {
	fallback := "no"
	if current {
		fallback = "yes"
	}
	raw, err := p.askString(label+" (yes/no)", fallback)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(raw) {
	case "yes", "y", "true":
		return true, nil
	case "no", "n", "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be yes or no", label)
	}
}
