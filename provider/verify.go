package provider

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-pkgz/rest"
	"github.com/golang-jwt/jwt"
	"github.com/microcosm-cc/bluemonday"

	"github.com/go-pkgz/auth/avatar"
	"github.com/go-pkgz/auth/logger"
	"github.com/go-pkgz/auth/token"
)

// VerifyHandler implements non-oauth2 provider authorizing users with some confirmation.
// can be email, IM or anything else implementing Sender interface
type VerifyHandler struct {
	logger.L
	ProviderName string
	TokenService VerifTokenService
	Issuer       string
	AvatarSaver  AvatarSaver
	UserSaver    func(token.User) error
	WithPassword bool
	Sender       Sender
	Template     *template.Template
	UseGravatar  bool
}

// Sender defines interface to send emails
type Sender interface {
	Send(address, text string) error
}

// SenderFunc type is an adapter to allow the use of ordinary functions as Sender.
type SenderFunc func(address, text string) error

// Send calls f(address,text) to implement Sender interface
func (f SenderFunc) Send(address, text string) error {
	return f(address, text)
}

// VerifTokenService defines interface accessing tokens
type VerifTokenService interface {
	Token(claims token.Claims) (string, error)
	Parse(tokenString string) (claims token.Claims, err error)
	IsExpired(claims token.Claims) bool
	Set(w http.ResponseWriter, claims token.Claims) (token.Claims, error)
	Get(r *http.Request) (claims token.Claims, token string, err error)
	Reset(w http.ResponseWriter)
}

// Name of the handler
func (e VerifyHandler) Name() string {
	return e.ProviderName
}

// LoginHandler gets name and address from query, makes confirmation token and sends it to user.
// In case if confirmation token presented in the query uses it to create auth token
func (e VerifyHandler) LoginHandler(w http.ResponseWriter, r *http.Request) {
	// GET /login?site=site&user=name&address=someone@example.com
	tkn := r.URL.Query().Get("token")
	if tkn == "" { // no token, ask confirmation via email
		e.sendConfirmation(w, r)
		return
	}

	// confirmation token presented
	// GET /login?token=confirmation-jwt&sess=1
	confClaims, err := e.TokenService.Parse(tkn)
	if err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusForbidden, err, "failed to verify confirmation token")
		return
	}

	if e.TokenService.IsExpired(confClaims) {
		rest.SendErrorJSON(w, r, e.L, http.StatusForbidden, fmt.Errorf("expired"), "failed to verify confirmation token")
		return
	}

	if confClaims.Handshake.State != "confirm" {
		rest.SendErrorJSON(w, r, e.L, http.StatusForbidden, fmt.Errorf("confirm"), "failed to verify confirmation token")
		return
	}

	elems := strings.Split(confClaims.Handshake.ID, "::")
	if len(elems) != 2 {
		rest.SendErrorJSON(w, r, e.L, http.StatusBadRequest, fmt.Errorf("%s", confClaims.Handshake.ID), "invalid handshake token")
		return
	}

	user, address := elems[0], elems[1]
	sessOnly := r.URL.Query().Get("session") == "1"

	if e.WithPassword {
		claims := token.Claims{
			Handshake: &token.Handshake{
				State: "credentials",
				ID:    confClaims.Handshake.ID,
			},
			User: &token.User{
				Name: user,
				ID:   e.ProviderName + "_" + token.HashID(sha1.New(), address),
			},
			SessionOnly: sessOnly,
			StandardClaims: jwt.StandardClaims{
				Audience:  e.sanitize(r.URL.Query().Get("site")),
				ExpiresAt: time.Now().Add(30 * time.Minute).Unix(),
				NotBefore: time.Now().Add(-1 * time.Minute).Unix(),
				Issuer:    e.Issuer,
			},
		}

		if _, err = e.TokenService.Set(w, claims); err != nil {
			rest.SendErrorJSON(w, r, e.L, http.StatusForbidden, err, "failed to set token")
			return
		}

		rest.RenderJSON(w, "confirmed")
		return
	}

	u := token.User{
		Name: user,
		ID:   e.ProviderName + "_" + token.HashID(sha1.New(), address),
	}
	// try to get gravatar for email
	if e.UseGravatar && strings.Contains(address, "@") { // TODO: better email check to avoid silly hits to gravatar api
		if picURL, e := avatar.GetGravatarURL(address); e == nil {
			u.Picture = picURL
		}
	}

	if u, err = setAvatar(e.AvatarSaver, u, &http.Client{Timeout: 5 * time.Second}); err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "failed to save avatar to proxy")
		return
	}

	if e.UserSaver != nil {
		err = e.UserSaver(u)
		if err != nil {
			rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "failed to save user")
			return
		}
	}

	cid, err := randToken()
	if err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "can't make token id")
		return
	}

	claims := token.Claims{
		User: &u,
		StandardClaims: jwt.StandardClaims{
			Id:       cid,
			Issuer:   e.Issuer,
			Audience: confClaims.Audience,
		},
		SessionOnly: sessOnly,
	}

	if _, err = e.TokenService.Set(w, claims); err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "failed to set token")
		return
	}
	if confClaims.Handshake != nil && confClaims.Handshake.From != "" {
		http.Redirect(w, r, confClaims.Handshake.From, http.StatusTemporaryRedirect)
		return
	}
	rest.RenderJSON(w, claims.User)
}

// GET /login?site=site&user=name&address=someone@example.com
func (e VerifyHandler) sendConfirmation(w http.ResponseWriter, r *http.Request) {
	user, address := r.URL.Query().Get("user"), r.URL.Query().Get("address")
	user = e.sanitize(user)
	address = e.sanitize(address)

	if user == "" || address == "" {
		rest.SendErrorJSON(w, r, e.L, http.StatusBadRequest, fmt.Errorf("wrong request"), "can't get user and address")
		return
	}

	claims := token.Claims{
		Handshake: &token.Handshake{
			State: "confirm",
			ID:    user + "::" + address,
		},
		SessionOnly: r.URL.Query().Get("session") != "" && r.URL.Query().Get("session") != "0",
		StandardClaims: jwt.StandardClaims{
			Audience:  e.sanitize(r.URL.Query().Get("site")),
			ExpiresAt: time.Now().Add(30 * time.Minute).Unix(),
			NotBefore: time.Now().Add(-1 * time.Minute).Unix(),
			Issuer:    e.Issuer,
		},
	}

	tkn, err := e.TokenService.Token(claims)
	if err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusForbidden, err, "failed to make login token")
		return
	}

	tmplData := struct {
		User    string
		Address string
		Token   string
		Site    string
	}{
		User:    user,
		Address: address,
		Token:   tkn,
		Site:    r.URL.Query().Get("site"),
	}
	buf := bytes.Buffer{}
	if err = e.Template.Execute(&buf, tmplData); err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "can't execute confirmation template")
		return
	}

	if err := e.Sender.Send(address, buf.String()); err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "failed to send confirmation")
		return
	}

	rest.RenderJSON(w, rest.JSON{"user": user, "address": address})
}

// AuthHandler doesn't do anything for direct login as it has no callbacks
func (e VerifyHandler) AuthHandler(w http.ResponseWriter, r *http.Request) {
	if !e.WithPassword {
		return
	}

	sessOnly := r.URL.Query().Get("session") == "1"

	claims, _, err := e.TokenService.Get(r)
	if err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "failed to get token")
		return
	}

	if claims.Handshake == nil || claims.Handshake.State != "credentials" {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "invalid kind of token")
		return
	}

	if e.UserSaver != nil {
		err = e.UserSaver(*claims.User)
		if err != nil {
			rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "failed to save user")
			return
		}
	}

	cid, err := randToken()
	if err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "can't make token id")
		return
	}

	authClaims := token.Claims{
		User: claims.User,
		StandardClaims: jwt.StandardClaims{
			Id:       cid,
			Issuer:   e.Issuer,
			Audience: claims.Audience,
		},
		SessionOnly: sessOnly,
	}

	if _, err = e.TokenService.Set(w, authClaims); err != nil {
		rest.SendErrorJSON(w, r, e.L, http.StatusInternalServerError, err, "failed to set token")
		return
	}

	rest.RenderJSON(w, authClaims.User)

}

// getPassword extracts password from request
func (e VerifyHandler) getPassword(w http.ResponseWriter, r *http.Request) (string, error) {
	// GET /something?user=name&passwd=xyz&aud=bar
	if r.Method == "GET" {
		return r.URL.Query().Get("passwd"), nil
	}

	if r.Method != "POST" {
		return "", fmt.Errorf("method %s not supported", r.Method)
	}

	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, MaxHTTPBodySize)
	}
	contentType := r.Header.Get("Content-Type")
	if contentType != "" {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			return "", err
		}
		contentType = mt
	}

	// POST with json body
	if contentType == "application/json" {
		var creds struct {
			Password string `json:"passwd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
			return "", fmt.Errorf("failed to parse request body: %w", err)
		}
		return creds.Password, nil
	}

	// POST with form
	if err := r.ParseForm(); err != nil {
		return "", fmt.Errorf("failed to parse request: %w", err)
	}

	return r.Form.Get("passwd"), nil
}

// LogoutHandler - GET /logout
func (e VerifyHandler) LogoutHandler(w http.ResponseWriter, _ *http.Request) {
	e.TokenService.Reset(w)
}

func (e VerifyHandler) sanitize(inp string) string {
	p := bluemonday.UGCPolicy()
	res := p.Sanitize(inp)
	res = template.HTMLEscapeString(res)
	res = strings.ReplaceAll(res, "&amp;", "&")
	res = strings.ReplaceAll(res, "&#34;", "\"")
	res = strings.ReplaceAll(res, "&#39;", "'")
	res = strings.ReplaceAll(res, "\n", "")
	res = strings.TrimSpace(res)
	if len(res) > 128 {
		return res[:128]
	}
	return res
}
