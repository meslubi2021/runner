package saml

import (
	"errors"
	"net/http"

	"github.com/crewjam/saml/samlsp"
	"github.com/deepsourcecorp/runner/auth"
	"github.com/labstack/echo/v4"
	"github.com/segmentio/ksuid"
)

type Handler struct {
	runner     *auth.Runner
	deepsource *auth.DeepSource
	middleware *samlsp.Middleware
	store      SessionStore
}

func NewHandler(runner *auth.Runner, deepsource *auth.DeepSource, middleware *samlsp.Middleware, store SessionStore) *Handler {
	return &Handler{
		runner:     runner,
		deepsource: deepsource,
		middleware: middleware,
		store:      store,
	}
}

type AuthorizationRequest struct {
	ClientID string
	Scopes   string
	State    string
}

func (r *AuthorizationRequest) Parse(req *http.Request) {
	q := req.URL.Query()
	r.ClientID = q.Get("client_id")
	r.Scopes = q.Get("scopes")
	r.State = q.Get("state")
}

func (h *Handler) SAMLHandler() echo.HandlerFunc {
	return echo.WrapHandler(h.middleware)
}

func (h *Handler) AuthorizationHandler() echo.HandlerFunc {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := new(AuthorizationRequest)
		request.Parse(r)
		if !h.runner.IsValidClientID(request.ClientID) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("invalid client_id"))
			return
		}

		_, err := h.middleware.Session.GetSession(r)
		if err == samlsp.ErrNoSession {
			h.middleware.HandleStartAuthFlow(w, r)
			return
		}

		if err != nil {
			h.middleware.OnError(w, r, err)
			return
		}

		token, _ := r.Cookie("token")
		if token == nil {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized"))
			return
		}

		session := NewSession()
		session.SetBackendToken(token.Value)
		if err := h.store.Create(session); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal server error"))
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    session.ID,
			Path:     "/",
			HttpOnly: true,
		})

		http.Redirect(w, r, "/apps/saml/oauth2/session?state="+request.State, http.StatusTemporaryRedirect)
	})

	return echo.WrapHandler(handler)
}

type SessionRequest struct {
	State     string `query:"state"`
	SessionID string
}

func (r *SessionRequest) Parse(c echo.Context) error {
	r.State = c.QueryParam("state")
	cookie, err := c.Cookie("session")
	if err != nil {
		return err
	}
	if cookie == nil {
		return errors.New("session cookie not found")
	}
	r.SessionID = cookie.Value
	return nil
}

func (h *Handler) HandleSession(c echo.Context) error {
	req := new(SessionRequest)
	if err := req.Parse(c); err != nil {
		return c.JSON(400, err.Error())
	}

	session, err := h.store.GetByID(req.SessionID)
	if err != nil {
		return c.JSON(400, err.Error())
	}

	code := ksuid.New().String()
	session.SetAccessCode(code)
	if err := h.store.Update(session); err != nil {
		return c.JSON(500, err.Error())
	}

	u := h.deepsource.Host.JoinPath("/accounts/runner/apps/saml/login/callback/bifrost/")
	q := u.Query()
	q.Add("app_id", "saml")
	q.Add("code", code)
	q.Add("state", req.State)
	u.RawQuery = q.Encode()
	return c.Redirect(http.StatusTemporaryRedirect, u.String())
}

type TokenRequest struct {
	Code         string `query:"code" json:"code"`
	ClientID     string `query:"client_id" json:"client_id"`
	ClientSecret string `query:"client_secret" json:"client_secret"`
}

func (h *Handler) HandleToken(c echo.Context) error {
	req := new(TokenRequest)
	if err := c.Bind(req); err != nil {
		return c.JSON(400, err.Error())
	}
	if !h.runner.IsValidClientID(req.ClientID) || !h.runner.IsValidClientSecret(req.ClientSecret) {
		return c.JSON(400, "invalid client_id or client_secret")
	}

	session, err := h.store.GetByAccessCode(req.Code)
	if err != nil {
		return c.JSON(500, err.Error())
	}
	session.UnsetAccessCode()
	session.GenerateRunnerToken(session.BackendToken.Expiry)

	err = h.store.Update(session)
	if err != nil {
		return c.JSON(500, err.Error())
	}
	return c.JSON(200, session.RunnerToken)
}
