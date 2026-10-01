package tools

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yui-companion/core/internal/ids"
	"github.com/yui-companion/core/internal/model"
)

// The built-in set from ADR-026. Risk levels follow SRS 16.5 exactly:
// reading is low, creating something the owner will see is medium, anything
// that reaches outside the machine is high and does not exist here yet.

// MemorySearcher is the slice of the memory service the tools need. Keeping it
// this narrow means a tool cannot wander into the rest of memory.
type MemorySearcher interface {
	SearchText(ctx context.Context, identityID, query string, limit int) ([]string, error)
}

// Reminder is a scheduled nudge owned by the user.
type Reminder struct {
	ID     string    `json:"id"`
	Text   string    `json:"text"`
	DueAt  time.Time `json:"due_at"`
	Done   bool      `json:"done"`
	Source string    `json:"source"`
}

// Note and Task are deliberately plain: the value is in being remembered and
// searchable, not in a task model nobody asked for.
type Note struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Done      bool       `json:"done"`
	DueAt     *time.Time `json:"due_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Workspace is the in-core store for reminders, notes and tasks. It is
// intentionally simple and local; a plugin can later replace it with a real
// calendar or task service behind the same tool names.
type Workspace struct {
	mu        sync.RWMutex
	reminders map[string]*Reminder
	notes     map[string]*Note
	tasks     map[string]*Task
	timers    map[string]time.Time
}

func NewWorkspace() *Workspace {
	return &Workspace{
		reminders: map[string]*Reminder{},
		notes:     map[string]*Note{},
		tasks:     map[string]*Task{},
		timers:    map[string]time.Time{},
	}
}

func (w *Workspace) Reminders() []*Reminder {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]*Reminder, 0, len(w.reminders))
	for _, r := range w.reminders {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DueAt.Before(out[j].DueAt) })
	return out
}

// Due returns reminders and timers that have come up, marking them done so the
// proactivity scheduler does not fire twice.
func (w *Workspace) Due(now time.Time) []*Reminder {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []*Reminder{}
	for _, r := range w.reminders {
		if !r.Done && !r.DueAt.After(now) {
			r.Done = true
			cp := *r
			out = append(out, &cp)
		}
	}
	return out
}

func (w *Workspace) Tasks() []*Task {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]*Task, 0, len(w.tasks))
	for _, t := range w.tasks {
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (w *Workspace) Notes() []*Note {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]*Note, 0, len(w.notes))
	for _, n := range w.notes {
		cp := *n
		out = append(out, &cp)
	}
	return out
}

// MediaController is implemented by a platform plugin. Absent means the tool
// reports honestly that it cannot act (NFR-010), instead of pretending.
type MediaController interface {
	Control(ctx context.Context, action string) error
}

// RegisterBuiltins installs the MVP tool set.
func RegisterBuiltins(r *Registry, ws *Workspace, mem MemorySearcher, media MediaController) error {
	object := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "number"}

	all := []Tool{
		{
			Name:        "time.now",
			Description: "Узнать текущие дату и время",
			Schema:      object(map[string]any{}),
			Risk:        model.RiskLow,
			Category:    model.CatProfile,
			Handler: func(_ context.Context, _ map[string]any) (string, error) {
				now := time.Now()
				return now.Format("Monday, 2 January 2006, 15:04 MST"), nil
			},
		},
		{
			Name:        "memory.search",
			Description: "Найти в долговременной памяти",
			Schema:      object(map[string]any{"query": str, "limit": num}),
			Required:    []string{"query"},
			Risk:        model.RiskLow,
			Category:    model.CatPreferences,
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				if mem == nil {
					return "", ErrNotImplemented
				}
				limit := 5
				if v, ok := args["limit"].(float64); ok && v > 0 {
					limit = int(v)
				}
				found, err := mem.SearchText(ctx, identityFrom(ctx), asString(args["query"]), limit)
				if err != nil {
					return "", err
				}
				if len(found) == 0 {
					return "В памяти ничего не найдено по этому запросу.", nil
				}
				return strings.Join(found, "\n"), nil
			},
		},
		{
			Name:        "reminder.create",
			Description: "Создать напоминание",
			Schema:      object(map[string]any{"text": str, "in_minutes": num, "at": str}),
			Required:    []string{"text"},
			// Medium: the owner will be interrupted later by this, so it is
			// confirmed once rather than assumed (SRS 16.5).
			Risk:     model.RiskMedium,
			Category: model.CatCalendar,
			Handler: func(_ context.Context, args map[string]any) (string, error) {
				due, err := resolveTime(args)
				if err != nil {
					return "", err
				}
				rem := &Reminder{ID: ids.New("rem"), Text: asString(args["text"]), DueAt: due, Source: "companion"}
				ws.mu.Lock()
				ws.reminders[rem.ID] = rem
				ws.mu.Unlock()
				return "Напоминание на " + due.Format("15:04 02.01") + ": " + rem.Text, nil
			},
		},
		{
			Name:        "reminder.list",
			Description: "Показать напоминания",
			Schema:      object(map[string]any{}),
			Risk:        model.RiskLow,
			Category:    model.CatCalendar,
			Handler: func(_ context.Context, _ map[string]any) (string, error) {
				items := ws.Reminders()
				if len(items) == 0 {
					return "Напоминаний нет.", nil
				}
				lines := make([]string, 0, len(items))
				for _, r := range items {
					status := ""
					if r.Done {
						status = " (выполнено)"
					}
					lines = append(lines, r.DueAt.Format("15:04 02.01")+" — "+r.Text+status)
				}
				return strings.Join(lines, "\n"), nil
			},
		},
		{
			Name:        "timer.start",
			Description: "Поставить таймер",
			Schema:      object(map[string]any{"minutes": num, "label": str}),
			Required:    []string{"minutes"},
			Risk:        model.RiskLow,
			Category:    model.CatCalendar,
			Handler: func(_ context.Context, args map[string]any) (string, error) {
				minutes, ok := args["minutes"].(float64)
				if !ok || minutes <= 0 || minutes > 24*60 {
					return "", errors.New("tools: minutes must be between 1 and 1440")
				}
				label := asString(args["label"])
				if label == "" {
					label = "таймер"
				}
				due := time.Now().Add(time.Duration(minutes) * time.Minute)
				rem := &Reminder{ID: ids.New("tmr"), Text: label, DueAt: due, Source: "timer"}
				ws.mu.Lock()
				ws.reminders[rem.ID] = rem
				ws.timers[rem.ID] = due
				ws.mu.Unlock()
				return label + " на " + due.Format("15:04"), nil
			},
		},
		{
			Name:        "note.append",
			Description: "Записать заметку",
			Schema:      object(map[string]any{"text": str}),
			Required:    []string{"text"},
			Risk:        model.RiskLow,
			Category:    model.CatFiles,
			Handler: func(_ context.Context, args map[string]any) (string, error) {
				note := &Note{ID: ids.New("note"), Text: asString(args["text"]), CreatedAt: time.Now().UTC()}
				ws.mu.Lock()
				ws.notes[note.ID] = note
				ws.mu.Unlock()
				return "Записала.", nil
			},
		},
		{
			Name:        "task.create",
			Description: "Добавить задачу",
			Schema:      object(map[string]any{"title": str, "in_minutes": num, "at": str}),
			Required:    []string{"title"},
			Risk:        model.RiskMedium,
			Category:    model.CatProjects,
			Handler: func(_ context.Context, args map[string]any) (string, error) {
				task := &Task{ID: ids.New("task"), Title: asString(args["title"]), CreatedAt: time.Now().UTC()}
				if due, err := resolveTime(args); err == nil {
					task.DueAt = &due
				}
				ws.mu.Lock()
				ws.tasks[task.ID] = task
				ws.mu.Unlock()
				return "Задача добавлена: " + task.Title, nil
			},
		},
		{
			Name:        "task.list",
			Description: "Показать задачи",
			Schema:      object(map[string]any{}),
			Risk:        model.RiskLow,
			Category:    model.CatProjects,
			Handler: func(_ context.Context, _ map[string]any) (string, error) {
				items := ws.Tasks()
				if len(items) == 0 {
					return "Задач нет.", nil
				}
				lines := make([]string, 0, len(items))
				for _, t := range items {
					mark := "[ ]"
					if t.Done {
						mark = "[x]"
					}
					lines = append(lines, mark+" "+t.Title)
				}
				return strings.Join(lines, "\n"), nil
			},
		},
		{
			Name:        "media.control",
			Description: "Управление воспроизведением музыки",
			Schema: object(map[string]any{
				"action": map[string]any{"type": "string", "enum": []string{"play", "pause", "next", "previous"}},
			}),
			Required: []string{"action"},
			Risk:     model.RiskLow,
			Category: model.CatActions,
			// Provided by a platform plugin; absent on a bare install.
			Capability: "media.control",
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				if media == nil {
					return "", ErrNotImplemented
				}
				action := asString(args["action"])
				switch action {
				case "play", "pause", "next", "previous":
				default:
					return "", ErrInvalidArgs
				}
				if err := media.Control(ctx, action); err != nil {
					return "", err
				}
				return "Готово: " + action, nil
			},
		},
		{
			Name:        "link.open",
			Description: "Открыть ссылку на компьютере",
			Schema:      object(map[string]any{"url": str}),
			Required:    []string{"url"},
			// Medium, not low: opening a link is a side effect the owner sees,
			// and a model-chosen URL is not a safe URL by default.
			Risk:       model.RiskMedium,
			Category:   model.CatActions,
			Capability: "desktop.open_link",
			Handler: func(_ context.Context, args map[string]any) (string, error) {
				raw := asString(args["url"])
				parsed, err := url.Parse(raw)
				if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
					return "", errors.New("tools: only http and https links may be opened")
				}
				// Execution belongs to a desktop capability, not to the core:
				// the core does not launch processes on the model's word.
				return "", ErrNotImplemented
			},
		},
	}

	for _, t := range all {
		if err := r.Register(t); err != nil {
			return err
		}
	}
	return nil
}

// resolveTime accepts either "in_minutes" or an absolute "at" in HH:MM or
// RFC3339. Ambiguity is an error, not a guess.
func resolveTime(args map[string]any) (time.Time, error) {
	if v, ok := args["in_minutes"].(float64); ok && v > 0 {
		return time.Now().Add(time.Duration(v) * time.Minute), nil
	}
	at := asString(args["at"])
	if at == "" {
		return time.Time{}, errors.New("tools: specify in_minutes or at")
	}
	if t, err := time.Parse(time.RFC3339, at); err == nil {
		return t, nil
	}
	if t, err := time.Parse("15:04", at); err == nil {
		now := time.Now()
		due := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
		if due.Before(now) {
			due = due.Add(24 * time.Hour)
		}
		return due, nil
	}
	return time.Time{}, errors.New("tools: could not read the time, use HH:MM or RFC3339")
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

type ctxKey string

const identityKey ctxKey = "identity_id"

// WithIdentity carries the calling personality into handlers that need it.
func WithIdentity(ctx context.Context, identityID string) context.Context {
	return context.WithValue(ctx, identityKey, identityID)
}

func identityFrom(ctx context.Context) string {
	v, _ := ctx.Value(identityKey).(string)
	return v
}
