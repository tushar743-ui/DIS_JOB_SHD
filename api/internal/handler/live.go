package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/tushar/dis-job-queue/shared/events"
)

const (
	clientBuffer  = 64
	pingInterval  = 25 * time.Second
	writeTimeout  = 10 * time.Second
	maxReadBytes  = 4096
	maxPerProject = 200
)

type subscriber struct {
	out    chan []byte
	closed chan struct{}
	once   sync.Once
}

func (s *subscriber) stop() { s.once.Do(func() { close(s.closed) }) }

type projectRoom struct {
	members map[*subscriber]struct{}
	cancel  context.CancelFunc
}

type Hub struct {
	rdb   *redis.Client
	mu    sync.Mutex
	rooms map[string]*projectRoom
	base  context.Context
	stop  context.CancelFunc
}

func NewHub(rdb *redis.Client) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{rdb: rdb, rooms: map[string]*projectRoom{}, base: ctx, stop: cancel}
}

func (h *Hub) Close() {
	h.stop()
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, room := range h.rooms {
		for sub := range room.members {
			sub.stop()
		}
		room.cancel()
	}
	h.rooms = map[string]*projectRoom{}
}

func (h *Hub) join(projectID string, sub *subscriber) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, ok := h.rooms[projectID]
	if !ok {
		ctx, cancel := context.WithCancel(h.base)
		room = &projectRoom{members: map[*subscriber]struct{}{}, cancel: cancel}
		h.rooms[projectID] = room
		go h.pump(ctx, projectID)
	}
	if len(room.members) >= maxPerProject {
		return false
	}
	room.members[sub] = struct{}{}
	return true
}

func (h *Hub) leave(projectID string, sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, ok := h.rooms[projectID]
	if !ok {
		return
	}
	delete(room.members, sub)
	sub.stop()
	if len(room.members) == 0 {
		room.cancel()
		delete(h.rooms, projectID)
	}
}

func (h *Hub) pump(ctx context.Context, projectID string) {
	stream := events.Subscribe(ctx, h.rdb, projectID)
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-stream:
			if !ok {
				return
			}
			payload, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			h.broadcast(projectID, payload)
		}
	}
}

func (h *Hub) broadcast(projectID string, payload []byte) {
	h.mu.Lock()
	room, ok := h.rooms[projectID]
	if !ok {
		h.mu.Unlock()
		return
	}
	slow := []*subscriber{}
	for sub := range room.members {
		select {
		case sub.out <- payload:
		default:
			slow = append(slow, sub)
		}
	}
	h.mu.Unlock()

	for _, sub := range slow {
		log.Warn().Str("project_id", projectID).Msg("dropping websocket client that cannot keep up")
		sub.stop()
	}
}

type LiveHandler struct{ hub *Hub }

func NewLiveHandler(hub *Hub) *LiveHandler { return &LiveHandler{hub: hub} }

func (l *LiveHandler) Stream(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	conn.SetReadLimit(maxReadBytes)

	sub := &subscriber{out: make(chan []byte, clientBuffer), closed: make(chan struct{})}
	if !l.hub.join(projectID, sub) {
		conn.Close(websocket.StatusTryAgainLater, "too many live connections for this project")
		return
	}
	defer l.hub.leave(projectID, sub)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	if err := writeFrame(ctx, conn, []byte(`{"type":"stream.ready"}`)); err != nil {
		return
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.closed:
			conn.Close(websocket.StatusPolicyViolation, "client too slow")
			return
		case payload := <-sub.out:
			if err := writeFrame(ctx, conn, payload); err != nil {
				return
			}
		case <-ping.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return
			}
		}
	}
}

func writeFrame(ctx context.Context, conn *websocket.Conn, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return conn.Write(ctx, websocket.MessageText, payload)
}
