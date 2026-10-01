package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yui-companion/core/internal/model"
)

// RegisterComputer installs a bridge to Microsoft's Fara harness. The same
// permission/confirmation gate as all other tools handles every start/reply.
func RegisterComputer(r *Registry, endpoint string, prepare func(context.Context) error) error {
	c := &computerClient{endpoint: strings.TrimRight(endpoint, "/"), http: &http.Client{Timeout: 20 * time.Second}}
	str := map[string]any{"type": "string"}
	for _, spec := range []struct {
		name, description, op string
		props                 map[string]any
		required              []string
	}{
		{"computer.browse", "Выполнить задачу в отдельном браузере через Fara. Укажи только нужные домены; обычные окна Windows недоступны.", "start", map[string]any{"task": str, "start_url": str, "allowed_hosts": map[string]any{"type": "array", "items": str}}, []string{"task", "start_url", "allowed_hosts"}},
		{"computer.reply", "Передать подтверждённый владельцем ответ на вопрос Fara и продолжить задачу", "reply", map[string]any{"id": str, "text": str}, []string{"id", "text"}},
		{"computer.cancel", "Остановить задачу Fara и закрыть её браузер", "cancel", map[string]any{"id": str}, []string{"id"}},
	} {
		spec := spec
		err := r.Register(Tool{Name: spec.name, Description: spec.description,
			Schema: map[string]any{"type": "object", "properties": spec.props}, Required: spec.required,
			Risk: model.RiskMedium, Category: model.CatActions,
			Handler: func(ctx context.Context, args map[string]any) (string, error) {
				if spec.op != "cancel" {
					if err := prepare(ctx); err != nil {
						return "", err
					}
				}
				payload := map[string]any{"identity_id": identityFrom(ctx)}
				for k, v := range args {
					payload[k] = v
				}
				job, err := c.call(ctx, spec.op, payload)
				if err != nil {
					return "", err
				}
				id, _ := job["id"].(string)
				poll := map[string]any{"identity_id": identityFrom(ctx), "id": id}
				ticker := time.NewTicker(500 * time.Millisecond)
				defer ticker.Stop()
				for job["status"] == "running" {
					select {
					case <-ctx.Done():
						cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						_, _ = c.call(cancelCtx, "cancel", poll)
						return "", ctx.Err()
					case <-ticker.C:
					}
					job, err = c.call(ctx, "status", poll)
					if err != nil {
						return "", err
					}
				}
				result, err := json.Marshal(job)
				return string(result), err
			}})
		if err != nil {
			return err
		}
	}
	return nil
}

type computerClient struct {
	endpoint string
	http     *http.Client
}

func (c *computerClient) call(ctx context.Context, op string, body map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/v1/computer/"+op, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("computer worker rejected request: " + resp.Status)
	}
	var out map[string]any
	err = json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&out)
	return out, err
}
