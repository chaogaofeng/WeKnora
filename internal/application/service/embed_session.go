package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/redis/go-redis/v9"
)

const (
	embedSessionTokenPrefix = "ems_"
	embedSessionRedisPrefix = "embed:session:"
	embedSessionTTL         = 30 * time.Minute
)

var ErrEmbedSessionUnavailable = errors.New("embed session tokens unavailable")

func generateEmbedSessionToken() (string, error) {
	buf := make([]byte, embedTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return embedSessionTokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// embedSessionTokenPayload is the Redis value behind an ems_ session token.
// It carries the channel id plus an optional host-asserted owner id so a chat
// session can be attributed to a logged-in user of the embedding project
// without trusting the browser.
type embedSessionTokenPayload struct {
	ChannelID string `json:"c"`
	Owner     string `json:"o"`
}

// IssueSessionToken mints a short-lived session token bound to channelID. When
// owner is non-empty (a host-asserted external user id supplied by the host
// backend that holds the publish token), it is embedded in the token so the
// value is verified at session-creation time rather than self-reported.
func (s *embedChannelService) IssueSessionToken(ctx context.Context, channelID, owner string) (string, int, error) {
	if s.redis == nil {
		return "", 0, ErrEmbedSessionUnavailable
	}
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return "", 0, ErrEmbedTokenInvalid
	}
	owner = strings.TrimSpace(owner)
	if owner != "" {
		if err := types.ValidateEmbedOwnerID(owner); err != nil {
			return "", 0, err
		}
	}
	token, err := generateEmbedSessionToken()
	if err != nil {
		return "", 0, err
	}
	payload := embedSessionTokenPayload{ChannelID: channelID, Owner: owner}
	val, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}
	key := embedSessionRedisPrefix + token
	if err := s.redis.Set(ctx, key, val, embedSessionTTL).Err(); err != nil {
		return "", 0, err
	}
	return token, int(embedSessionTTL.Seconds()), nil
}

// ResolveSessionToken returns the channel ID and host-asserted owner stored for
// a session token. Older tokens (before owner support) stored the bare channel
// id; those resolve with an empty owner for backward compatibility.
func (s *embedChannelService) ResolveSessionToken(ctx context.Context, token string) (string, string, error) {
	token = strings.TrimSpace(token)
	if !strings.HasPrefix(token, embedSessionTokenPrefix) {
		return "", "", ErrEmbedTokenInvalid
	}
	if s.redis == nil {
		return "", "", ErrEmbedSessionUnavailable
	}
	raw, err := s.redis.Get(ctx, embedSessionRedisPrefix+token).Result()
	if err == redis.Nil {
		return "", "", ErrEmbedTokenInvalid
	}
	if err != nil {
		return "", "", err
	}
	var payload embedSessionTokenPayload
	if jsonErr := json.Unmarshal([]byte(raw), &payload); jsonErr != nil || payload.ChannelID == "" {
		// Legacy token: the raw value is the channel id.
		channelID := strings.TrimSpace(raw)
		if channelID == "" {
			return "", "", ErrEmbedTokenInvalid
		}
		return channelID, "", nil
	}
	return payload.ChannelID, payload.Owner, nil
}

// LookupEnabledChannel loads an embed channel and verifies it is enabled.
func (s *embedChannelService) LookupEnabledChannel(ctx context.Context, channelID string) (*types.EmbedChannel, error) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil, ErrEmbedTokenInvalid
	}
	ch, err := s.repo.GetByID(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, ErrEmbedTokenInvalid
	}
	if !ch.Enabled {
		return nil, ErrEmbedChannelDisabled
	}
	return ch, nil
}

// IsEmbedSessionToken reports whether token is a session token (ems_ prefix).
func IsEmbedSessionToken(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), embedSessionTokenPrefix)
}

// SignEmbedSessionHandle binds a chat session id to its embed channel with an
// HMAC keyed by a server-only deployment secret. The handle is handed
// to the widget at session-creation time and must be presented on every history
// load / chat call. Because the session id travels in the request path (and can
// land in access logs), this signature — sent in a header, never logged — is the
// real authorization secret: a leaked session id is useless without it. Rotating
// the channel token invalidates outstanding handles, which is acceptable.
func SignEmbedSessionHandle(ch *types.EmbedChannel, sessionID string) string {
	if ch == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	key := utils.SystemHMACKey()
	if len(key) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("embed-session:v2|" + ch.PublishToken + "|" + ch.ID + "|" + sessionID))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyEmbedSessionHandle reports whether sig is a valid handle for sessionID
// on channel ch, using a constant-time comparison.
func VerifyEmbedSessionHandle(ch *types.EmbedChannel, sessionID, sig string) bool {
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return false
	}
	expected := SignEmbedSessionHandle(ch, sessionID)
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(sig)) == 1
}

// IssuePreviewSession mints a short-lived session token for management UI preview.
func (s *embedChannelService) IssuePreviewSession(
	ctx context.Context, tenantID uint64, channelID string,
) (string, int, error) {
	ch, err := s.getOwned(ctx, tenantID, channelID)
	if err != nil {
		return "", 0, err
	}
	if !ch.Enabled {
		return "", 0, ErrEmbedChannelDisabled
	}
	return s.IssueSessionToken(ctx, ch.ID, "")
}
