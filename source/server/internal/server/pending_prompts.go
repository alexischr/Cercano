package server

import (
	"sort"
	"sync"

	"cercano/source/server/pkg/proto"
	protobuf "google.golang.org/protobuf/proto"
)

// pendingPromptHub retains only live approval prompts, independently of stream
// replay. Subscribers receive coalesced change signals, not lossy prompt data;
// each signal causes a fresh snapshot. No subscriber can block the requester.
// All prompt pointers stored here are immutable and owned by the hub.
type pendingPromptHub struct {
	mu    sync.Mutex
	chats map[string]*pendingPromptChat
}
type pendingPromptChat struct {
	prompts     map[string]*proto.PermissionRequired
	subscribers map[chan struct{}]struct{}
}

func (h *pendingPromptHub) chatLocked(conv string) *pendingPromptChat {
	if h.chats == nil {
		h.chats = make(map[string]*pendingPromptChat)
	}
	chat := h.chats[conv]
	if chat == nil {
		chat = &pendingPromptChat{prompts: make(map[string]*proto.PermissionRequired), subscribers: make(map[chan struct{}]struct{})}
		h.chats[conv] = chat
	}
	return chat
}
func signalPrompts(chat *pendingPromptChat) {
	for ch := range chat.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (h *pendingPromptHub) publish(conv string, prompt *proto.PermissionRequired) func() {
	copy := protobuf.Clone(prompt).(*proto.PermissionRequired)
	h.mu.Lock()
	chat := h.chatLocked(conv)
	chat.prompts[copy.ToolUseId] = copy
	signalPrompts(chat)
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if chat.prompts[copy.ToolUseId] == copy {
			delete(chat.prompts, copy.ToolUseId)
			signalPrompts(chat)
		}
		h.pruneLocked(conv, chat)
	}
}
func (h *pendingPromptHub) pruneLocked(conv string, chat *pendingPromptChat) {
	if len(chat.prompts) == 0 && len(chat.subscribers) == 0 && h.chats[conv] == chat {
		delete(h.chats, conv)
	}
}
func (h *pendingPromptHub) snapshot(conv string) []*proto.PermissionRequired {
	h.mu.Lock()
	defer h.mu.Unlock()
	chat := h.chats[conv]
	if chat == nil {
		return nil
	}
	ids := make([]string, 0, len(chat.prompts))
	for id := range chat.prompts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*proto.PermissionRequired, 0, len(ids))
	for _, id := range ids {
		out = append(out, protobuf.Clone(chat.prompts[id]).(*proto.PermissionRequired))
	}
	return out
}
func (h *pendingPromptHub) subscribe(conv string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	chat := h.chatLocked(conv)
	chat.subscribers[ch] = struct{}{}
	ch <- struct{}{}
	h.mu.Unlock()
	return ch, func() { h.mu.Lock(); defer h.mu.Unlock(); delete(chat.subscribers, ch); h.pruneLocked(conv, chat) }
}
