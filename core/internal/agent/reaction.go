package agent

import "strings"

// ReactionParser removes the optional emotion tag before text reaches the UI,
// memory or TTS. It handles a tag split across streamed network chunks.
type ReactionParser struct {
	buffer   string
	decided  bool
	dropping bool
	Emotion  string
}

func (p *ReactionParser) Feed(part string) string {
	if p.dropping {
		if end := strings.IndexByte(part, ']'); end >= 0 {
			p.dropping = false
			p.decided = true
			return strings.TrimLeft(part[end+1:], " \t\r\n")
		}
		return ""
	}
	if p.decided {
		return part
	}
	p.buffer += part
	trimmed := strings.TrimLeft(p.buffer, " \t\r\n")
	const tag = "[emotion:"
	if trimmed == "" || (len(trimmed) < len(tag) && strings.HasPrefix(tag, strings.ToLower(trimmed))) {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), tag) {
		p.decided = true
		text := p.buffer
		p.buffer = ""
		return text
	}
	end := strings.IndexByte(trimmed, ']')
	if end < 0 && len(trimmed) <= 40 {
		return ""
	}
	if end < 0 {
		// Drop an oversized metadata field without buffering the rest of it.
		p.dropping = true
		p.buffer = ""
		return ""
	}
	if end <= 40 {
		label := strings.TrimSpace(strings.ToLower(trimmed[len(tag):end]))
		switch label {
		case "joy", "warm", "concern", "sad", "alert", "surprise", "think", "calm", "neutral", "angry":
			p.Emotion = label
		}
	}
	p.decided = true
	p.buffer = ""
	return strings.TrimLeft(trimmed[end+1:], " \t\r\n")
}

func (p *ReactionParser) Flush() string {
	if p.decided {
		return ""
	}
	p.decided = true
	text := p.buffer
	p.buffer = ""
	if p.dropping || strings.HasPrefix(strings.ToLower(strings.TrimLeft(text, " \t\r\n")), "[emotion:") {
		return ""
	}
	return text
}
