package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PejarRu/mcp-aules/internal/api"
	"github.com/PejarRu/mcp-aules/internal/config"
	"github.com/PejarRu/mcp-aules/internal/oauth"
	"github.com/PejarRu/mcp-aules/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

const keepaliveInterval = 30 * time.Minute

type clientKey struct{}
type app struct {
	cfg   config.Config
	store *oauth.Store
	mcp   http.Handler
}
type authPage struct {
	ClientID, RedirectURI, State, Challenge, ChallengeMethod, Resource, Scope, MoodleURL, Error string
	Platforms                                                                                   []config.MoodlePlatform
}

type callbackPage struct {
	URL string
}

var callbackHandoffTemplate = template.Must(template.New("callback-handoff").Parse(`<!doctype html><html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover"><meta http-equiv="refresh" content="1;url={{.URL}}"><title>Volver a ChatGPT</title><style>*{box-sizing:border-box}body{margin:0;min-height:100vh;min-height:100svh;padding:1.25rem;display:grid;place-items:center;background:radial-gradient(ellipse at top,#e1f1ff 0,transparent 58%),linear-gradient(145deg,#f4f8fc,#edf4f8);color:#15243a;font:16px/1.5 system-ui,-apple-system,BlinkMacSystemFont,sans-serif}main{width:min(100%,440px);padding:clamp(1.4rem,6vw,2rem);border:1px solid #dbe6ef;border-radius:22px;background:#fff;box-shadow:0 18px 50px #153a5718;text-align:center}h1{margin:.4rem 0;color:#125d9b;font-size:1.5rem}p{color:#526479}a{display:block;margin-top:1.4rem;padding:.9rem 1rem;border-radius:12px;background:linear-gradient(100deg,#0877bd,#1268aa);color:white;font-weight:700;text-decoration:none;touch-action:manipulation}.hint{font-size:.9rem;color:#718093}</style></head><body><main><h1>Conexión con Aules completada</h1><p>Vuelve a ChatGPT para terminar de conectar.</p><a href="{{.URL}}">Volver a ChatGPT</a><p class="hint">Si no vuelves automáticamente, pulsa el botón.</p></main></body></html>`))

var platformLoginTemplate = template.Must(template.New("platform-login").Parse(`<!doctype html><html lang="es"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Conectar Aules</title><style>*{box-sizing:border-box}body{margin:0;min-height:100vh;min-height:100svh;padding:max(1.25rem,env(safe-area-inset-top)) 1rem max(1.25rem,env(safe-area-inset-bottom));display:grid;place-items:center;background:radial-gradient(ellipse at top,#e1f1ff 0,transparent 58%),linear-gradient(145deg,#f4f8fc,#edf4f8);color:#15243a;font:16px/1.5 system-ui,-apple-system,BlinkMacSystemFont,sans-serif}body>h1,body>p{width:min(100%,440px)}body>h1{margin:0;color:#125d9b;font-size:1.75rem}body>p{margin:.3rem 0;color:#526479}form{width:min(100%,440px);padding:clamp(1.15rem,5vw,2rem);background:#fff;border:1px solid #dbe6ef;border-radius:22px;box-shadow:0 18px 50px #153a5718}label{display:block;margin:1rem 0 .45rem;font-weight:650}input,select{display:block;width:100%;min-height:52px;padding:.8rem .9rem;border:1px solid #b8c7d5;border-radius:12px;background:#fff;color:inherit;font:inherit;font-size:16px}input:focus,select:focus{outline:3px solid #1683cb2e;border-color:#1683cb}button{display:block;width:100%;min-height:54px;margin-top:1.35rem;padding:.85rem;border:0;border-radius:12px;background:linear-gradient(100deg,#0877bd,#1268aa);color:#fff;font:700 1rem system-ui,sans-serif;cursor:pointer;touch-action:manipulation}button:hover{filter:brightness(.96)}.error{padding:.85rem 1rem;border:1px solid #efc2c2;border-radius:12px;background:#fff2f2;color:#8b2424}.hint{color:#68788a;font-size:.9rem}.privacy{margin:1rem 0 0;color:#718093;font-size:.82rem}</style><h1>Conectar Aules</h1>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form method="post" action="/authorize"><input type="hidden" name="client_id" value="{{.ClientID}}"><input type="hidden" name="redirect_uri" value="{{.RedirectURI}}"><input type="hidden" name="state" value="{{.State}}"><input type="hidden" name="code_challenge" value="{{.Challenge}}"><input type="hidden" name="code_challenge_method" value="{{.ChallengeMethod}}"><input type="hidden" name="resource" value="{{.Resource}}"><input type="hidden" name="scope" value="{{.Scope}}"><label for="moodle_url">Plataforma</label><select id="moodle_url" name="moodle_url">{{range .Platforms}}<option value="{{.URL}}"{{if eq .URL $.MoodleURL}} selected{{end}}>{{.Name}}</option>{{end}}</select><label for="username">Usuario de Aules</label><input id="username" name="username" autocomplete="username" autocapitalize="none" spellcheck="false"><p class="hint">Usa tu nombre de usuario, no el correo electrónico.</p><label for="password">Contraseña</label><input id="password" name="password" type="password" autocomplete="current-password"><button type="submit" formnovalidate>Conectar con Aules</button><p class="privacy">Las credenciales se envían directamente a Aules. La contraseña no se almacena.</p></form>`))

func runKeepalive(ctx context.Context, store *oauth.Store) {
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sessions, err := store.KeepaliveSessions()
			if err != nil {
				log.Printf("Aules keepalive session lookup failed: %v", err)
				continue
			}
			for _, session := range sessions {
				client := api.NewClient()
				client.SetSession(session.MoodleURL, session.Token)
				requestCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
				_, err := client.Call(requestCtx, "core_webservice_get_site_info", nil)
				cancel()
				if err != nil {
					log.Printf("Aules keepalive failed for platform %s: %v", session.MoodleURL, err)
				}
			}
		}
	}
}

func main() {
	cfg := config.LoadFromEnv()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	store, err := oauth.Open(cfg.DatabasePath, cfg.EncryptionKey)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if cfg.KeepaliveEnabled {
		go runKeepalive(context.Background(), store)
	}
	a := &app{cfg: cfg, store: store}
	a.mcp = mcpserver.NewStreamableHTTPServer(newMCPServer(), mcpserver.WithStateLess(true))
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", a.landing)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", a.authorizationMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", a.resourceMetadata)
	mux.HandleFunc("POST /register", a.register)
	mux.HandleFunc("GET /authorize", a.authorizeGet)
	mux.HandleFunc("POST /authorize", a.authorizePost)
	mux.HandleFunc("POST /token", a.token)
	mux.HandleFunc("POST /revoke", a.revoke)
	mux.Handle("/mcp", chatGPTCORS(a.bearer(http.HandlerFunc(a.mcp.ServeHTTP))))
	srv := &http.Server{Addr: cfg.ListenAddr, Handler: securityHeaders(mux), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	log.Printf("listening on %s", cfg.ListenAddr)
	log.Fatal(srv.ListenAndServe())
}

func (a *app) landing(w http.ResponseWriter, r *http.Request) {
	endpoint := a.cfg.PublicBaseURL + "/mcp"
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":             "invalid_mcp_endpoint",
			"error_description": "Use the /mcp endpoint, not the base URL.",
			"mcp_endpoint":      endpoint,
		})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="es"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>URL de Aules MCP</title><body><main><h1>Esta no es la URL del servidor MCP</h1><p>En ChatGPT u otro cliente MCP, usa exactamente esta URL:</p><p><code>%s</code></p><p>La URL debe terminar en <code>/mcp</code>. No uses solo el dominio.</p></main></body></html>`, html.EscapeString(endpoint))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func (a *app) authorizationMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"issuer": a.cfg.PublicBaseURL, "authorization_endpoint": a.cfg.PublicBaseURL + "/authorize", "token_endpoint": a.cfg.PublicBaseURL + "/token", "registration_endpoint": a.cfg.PublicBaseURL + "/register", "revocation_endpoint": a.cfg.PublicBaseURL + "/revoke", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": []string{"mcp"}, "authorization_response_iss_parameter_supported": true})
}
func (a *app) resourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"resource": a.cfg.PublicBaseURL + "/mcp", "authorization_servers": []string{a.cfg.PublicBaseURL}, "bearer_methods_supported": []string{"header"}})
}
func (a *app) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RedirectURIs []string `json:"redirect_uris"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in) != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_client_metadata"})
		return
	}
	c, err := a.store.Register(in.RedirectURIs)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_client_metadata", "error_description": err.Error()})
		return
	}
	writeJSON(w, 201, map[string]any{"client_id": c.ID, "redirect_uris": c.RedirectURIs, "token_endpoint_auth_method": "none"})
}
func (a *app) authInput(r *http.Request) authPage {
	moodleURL := r.FormValue("moodle_url")
	if moodleURL == "" {
		moodleURL = a.cfg.MoodleURL
	}
	resource := r.FormValue("resource")
	if resource == "" {
		resource = a.cfg.PublicBaseURL + "/mcp"
	}
	return authPage{ClientID: r.FormValue("client_id"), RedirectURI: r.FormValue("redirect_uri"), State: r.FormValue("state"), Challenge: r.FormValue("code_challenge"), ChallengeMethod: r.FormValue("code_challenge_method"), Resource: resource, Scope: r.FormValue("scope"), MoodleURL: moodleURL, Platforms: config.MoodlePlatforms}
}
func (a *app) validateAuth(p authPage, responseType string) error {
	if responseType != "code" {
		return fmt.Errorf("response_type must be code")
	}
	if err := a.store.ValidateClient(p.ClientID, p.RedirectURI); err != nil {
		return err
	}
	if !config.IsSupportedMoodleURL(p.MoodleURL) {
		return fmt.Errorf("unsupported Aules platform")
	}
	if p.Resource != a.cfg.PublicBaseURL+"/mcp" {
		return fmt.Errorf("invalid resource")
	}
	return oauth.ValidatePKCE(p.Challenge, p.ChallengeMethod)
}
func (a *app) authorizeGet(w http.ResponseWriter, r *http.Request) {
	p := a.authInput(r)
	if err := a.validateAuth(p, r.URL.Query().Get("response_type")); err != nil {
		log.Printf("OAuth authorize request rejected: %v", err)
		http.Error(w, "invalid authorization request", 400)
		return
	}
	log.Printf("OAuth authorize page opened for client %s", shortID(p.ClientID))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	platformLoginTemplate.Execute(w, p)
}
func (a *app) authorizePost(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	p := a.authInput(r)
	if err := a.validateAuth(p, "code"); err != nil {
		log.Printf("OAuth authorize submission rejected: %v", err)
		http.Error(w, "invalid authorization request", 400)
		return
	}
	username, password := r.FormValue("username"), r.FormValue("password")
	if username == "" || password == "" {
		log.Printf("OAuth Aules authentication rejected for client %s: empty credentials", shortID(p.ClientID))
		p.Error = "Introduce tu usuario de Aules y contraseña."
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		platformLoginTemplate.Execute(w, p)
		return
	}
	log.Printf("OAuth Aules authentication started for client %s", shortID(p.ClientID))
	token, err := api.GetTokenFromCredentials(r.Context(), p.MoodleURL, username, password)
	if err != nil {
		log.Printf("OAuth Aules authentication failed for client %s after %s: %v", shortID(p.ClientID), time.Since(started).Round(time.Millisecond), err)
		p.Error = "No se pudo iniciar sesión. Comprueba el usuario de Aules y la contraseña e inténtalo de nuevo."
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		platformLoginTemplate.Execute(w, p)
		return
	}
	code, err := a.store.CreateCode(p.ClientID, p.RedirectURI, p.Challenge, p.MoodleURL, p.Resource, token)
	if err != nil {
		log.Printf("OAuth authorization code creation failed for client %s: %v", shortID(p.ClientID), err)
		http.Error(w, "authorization failed", 500)
		return
	}
	log.Printf("OAuth authorization succeeded for client %s after %s; callback host=%s state=%t resource=%t", shortID(p.ClientID), time.Since(started).Round(time.Millisecond), targetHost(p.RedirectURI), p.State != "", p.Resource != "")
	target, _ := url.Parse(p.RedirectURI)
	q := target.Query()
	q.Set("code", code)
	if p.State != "" {
		q.Set("state", p.State)
	}
	q.Set("iss", a.cfg.PublicBaseURL)
	target.RawQuery = q.Encode()
	if isMobileUserAgent(r.UserAgent()) {
		log.Printf("OAuth callback handoff displayed for mobile client %s; callback host=%s", shortID(p.ClientID), targetHost(target.String()))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if err := callbackHandoffTemplate.Execute(w, callbackPage{URL: target.String()}); err != nil {
			log.Printf("OAuth callback handoff rendering failed for client %s: %v", shortID(p.ClientID), err)
		}
		return
	}
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func isMobileUserAgent(userAgent string) bool {
	userAgent = strings.ToLower(userAgent)
	return strings.Contains(userAgent, "iphone") || strings.Contains(userAgent, "ipad") || strings.Contains(userAgent, "ipod") || strings.Contains(userAgent, "android") || strings.Contains(userAgent, "mobile")
}

func targetHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid"
	}
	return u.Host
}
func (a *app) token(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() != nil || r.FormValue("grant_type") != "authorization_code" {
		log.Printf("OAuth token request rejected: invalid request")
		writeJSON(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	clientID := r.FormValue("client_id")
	log.Printf("OAuth token exchange started for client %s", shortID(clientID))
	token, expires, resource, err := a.store.Exchange(r.FormValue("code"), clientID, r.FormValue("redirect_uri"), r.FormValue("code_verifier"), r.FormValue("resource"))
	if err != nil {
		log.Printf("OAuth token exchange failed for client %s: %v", shortID(clientID), err)
		writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
		return
	}
	log.Printf("OAuth token exchange succeeded for client %s", shortID(clientID))
	writeJSON(w, 200, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": expires - time.Now().Unix(), "scope": "mcp", "resource": resource})
}
func (a *app) revoke(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if r.ParseForm() == nil {
		a.store.Revoke(r.FormValue("token"))
	}
	w.WriteHeader(200)
}
func (a *app) bearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer, err := oauth.Bearer(r.Header.Get("Authorization"))
		if err != nil {
			log.Printf("MCP request rejected: missing bearer")
			unauthorized(w, a.cfg.PublicBaseURL)
			return
		}
		session, err := a.store.AulesSession(bearer)
		if err != nil {
			log.Printf("MCP request rejected: invalid bearer")
			unauthorized(w, a.cfg.PublicBaseURL)
			return
		}
		log.Printf("MCP request accepted: method=%s platform=%s", r.Method, session.MoodleURL)
		client := api.NewClient()
		client.SetSession(session.MoodleURL, session.Token)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientKey{}, client)))
	})
}
func unauthorized(w http.ResponseWriter, base string) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`, base))
	writeJSON(w, 401, map[string]string{"error": "invalid_token"})
}

func chatGPTCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "https://chatgpt.com" || origin == "https://chat.openai.com" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
			w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id")
			w.Header().Set("Vary", "Origin, Access-Control-Request-Headers")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self' https://chatgpt.com; style-src 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}
func requestClient(ctx context.Context) *api.Client { return ctx.Value(clientKey{}).(*api.Client) }

func newMCPServer() *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer("moodle-mcp-server", "2.0.0",
		mcpserver.WithToolCapabilities(true),
		mcpserver.WithTitle("Aules MCP"),
		mcpserver.WithInstructions("Use the read-only Aules tools to retrieve the connected user's academic information. When planning tasks, call list_courses and get_assignments for each relevant course. Treat submitted and submitted_late as already delivered; draft as unfinished; not_submitted as no submission recorded; status_unavailable as unknown. A passed due date alone does not mean the assignment was not submitted. Do not plan delivered assignments as pending."),
	)
	add := func(tool mcp.Tool, fn func(context.Context, mcp.CallToolRequest, *api.Client) (string, error)) {
		tool.Title = strings.ReplaceAll(tool.Name, "_", " ")
		tool.OutputSchema = mcp.ToolOutputSchema{Type: "object", Properties: map[string]any{}, AdditionalProperties: true}
		tool.Annotations = mcp.ToolAnnotation{
			Title:           tool.Title,
			ReadOnlyHint:    mcp.ToBoolPtr(true),
			DestructiveHint: mcp.ToBoolPtr(false),
			OpenWorldHint:   mcp.ToBoolPtr(false),
		}
		s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			out, err := fn(ctx, req, requestClient(ctx))
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(out), nil
		})
	}
	add(mcp.NewTool("get_site_info", mcp.WithDescription("Get Moodle site information")), func(c context.Context, _ mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetSiteInfo(c, a, tools.GetSiteInfoInput{})
	})
	add(mcp.NewTool("get_user_profile", mcp.WithDescription("Get current user profile")), func(c context.Context, _ mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetUserProfile(c, a, tools.GetUserProfileInput{})
	})
	add(mcp.NewTool("list_courses", mcp.WithDescription("List enrolled courses")), func(c context.Context, _ mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleListCourses(c, a, tools.ListCoursesInput{})
	})
	course := func(name, desc string, fn func(context.Context, *api.Client, int) (string, error)) {
		add(mcp.NewTool(name, mcp.WithDescription(desc), mcp.WithNumber("course_id", mcp.Required())), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
			return fn(c, a, intArg(r, "course_id"))
		})
	}
	course("get_course_contents", "Get course sections and activities", func(c context.Context, a *api.Client, id int) (string, error) {
		return tools.HandleGetCourseContents(c, a, tools.GetCourseContentsInput{CourseID: id})
	})
	course("get_course_details", "Get course details", func(c context.Context, a *api.Client, id int) (string, error) {
		return tools.HandleGetCourseDetails(c, a, tools.GetCourseDetailsInput{CourseID: id})
	})
	course("get_grades", "Get course grades", func(c context.Context, a *api.Client, id int) (string, error) {
		return tools.HandleGetGrades(c, a, tools.GetGradesInput{CourseID: id})
	})
	course("get_course_gradebook", "Get complete course gradebook", func(c context.Context, a *api.Client, id int) (string, error) {
		return tools.HandleGetCourseGradebook(c, a, tools.GetCourseGradebookInput{CourseID: id})
	})
	course("get_course_learning_content", "Get ordered course learning content", func(c context.Context, a *api.Client, id int) (string, error) {
		return tools.HandleGetCourseLearningContent(c, a, tools.GetCourseLearningContentInput{CourseID: id})
	})
	add(mcp.NewTool("get_grades_overview", mcp.WithDescription("Get grades overview")), func(c context.Context, _ mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetGradesOverview(c, a, tools.GetGradesOverviewInput{})
	})
	course("get_assignments", "List course assignments including your Moodle submission status, grading state, actual submitted time, attempt, and separate late vs overdue indicators. Use before planning to avoid repeating work already handed in. status_unavailable means unknown, not missing.", func(c context.Context, a *api.Client, id int) (string, error) {
		return tools.HandleGetAssignments(c, a, tools.GetAssignmentsInput{CourseID: id})
	})
	add(mcp.NewTool("get_upcoming_assignments", mcp.WithDescription("Get upcoming assignments"), mcp.WithNumber("days_ahead")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetUpcomingAssignments(c, a, tools.GetUpcomingAssignmentsInput{DaysAhead: intArg(r, "days_ahead")})
	})
	add(mcp.NewTool("get_assignment_details", mcp.WithDescription("Get complete assignment details and teacher attachments"), mcp.WithNumber("assignment_id", mcp.Required())), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetAssignmentDetails(c, a, tools.GetAssignmentDetailsInput{AssignmentID: intArg(r, "assignment_id")})
	})
	add(mcp.NewTool("get_assignment_status", mcp.WithDescription("Get assignment submission and grading status"), mcp.WithNumber("assignment_id", mcp.Required())), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetAssignmentStatus(c, a, tools.GetAssignmentStatusInput{AssignmentID: intArg(r, "assignment_id")})
	})
	course("list_quizzes", "List course quizzes", func(c context.Context, a *api.Client, id int) (string, error) {
		return tools.HandleListQuizzes(c, a, tools.ListQuizzesInput{CourseID: id})
	})
	add(mcp.NewTool("get_quiz_status", mcp.WithDescription("Get quiz configuration, attempts, and best grade"), mcp.WithNumber("quiz_id", mcp.Required())), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetQuizStatus(c, a, tools.GetQuizStatusInput{QuizID: intArg(r, "quiz_id")})
	})
	add(mcp.NewTool("list_course_announcements", mcp.WithDescription("List full course announcements"), mcp.WithNumber("course_id", mcp.Required()), mcp.WithNumber("limit")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleListCourseAnnouncements(c, a, tools.ListCourseAnnouncementsInput{CourseID: intArg(r, "course_id"), Limit: intArg(r, "limit")})
	})
	add(mcp.NewTool("list_resources", mcp.WithDescription("List course resources"), mcp.WithNumber("course_id", mcp.Required()), mcp.WithString("mime_type")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleListResources(c, a, tools.ListResourcesInput{CourseID: intArg(r, "course_id"), MimeType: mcp.ParseString(r, "mime_type", "")})
	})
	add(mcp.NewTool("read_resource", mcp.WithDescription("Download and extract text from a course resource (PDF, text, HTML, DOCX)"), mcp.WithNumber("course_id", mcp.Required()), mcp.WithNumber("module_id", mcp.Required()), mcp.WithNumber("max_chars")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleReadResource(c, a, tools.ReadResourceInput{CourseID: intArg(r, "course_id"), ModuleID: intArg(r, "module_id"), MaxChars: intArg(r, "max_chars")})
	})
	add(mcp.NewTool("get_recent_changes", mcp.WithDescription("Get recent course changes since a timestamp"), mcp.WithNumber("course_id"), mcp.WithNumber("since")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetRecentChanges(c, a, tools.GetRecentChangesInput{CourseID: intArg(r, "course_id"), Since: int64(mcp.ParseInt(r, "since", 0))})
	})
	add(mcp.NewTool("read_forum", mcp.WithDescription("Read forums: list course forums, list discussions, or full thread posts"), mcp.WithNumber("course_id"), mcp.WithNumber("forum_id"), mcp.WithNumber("discussion_id"), mcp.WithNumber("limit")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleReadForum(c, a, tools.ReadForumInput{CourseID: intArg(r, "course_id"), ForumID: intArg(r, "forum_id"), DiscussionID: intArg(r, "discussion_id"), Limit: intArg(r, "limit")})
	})
	add(mcp.NewTool("get_calendar_events", mcp.WithDescription("Get calendar events"), mcp.WithNumber("days_ahead")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetCalendarEvents(c, a, tools.GetCalendarEventsInput{DaysAhead: intArg(r, "days_ahead")})
	})
	add(mcp.NewTool("get_upcoming_deadlines", mcp.WithDescription("Get upcoming deadlines"), mcp.WithNumber("days_ahead")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetUpcomingDeadlines(c, a, tools.GetUpcomingDeadlinesInput{DaysAhead: intArg(r, "days_ahead")})
	})
	add(mcp.NewTool("get_notifications", mcp.WithDescription("Get Moodle notifications such as assignment and course alerts"), mcp.WithNumber("limit"), mcp.WithBoolean("unread_only")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetNotifications(c, a, tools.GetNotificationsInput{Limit: intArg(r, "limit"), UnreadOnly: mcp.ParseBoolean(r, "unread_only", false)})
	})
	add(mcp.NewTool("get_messages", mcp.WithDescription("Get private messages from other Moodle users"), mcp.WithNumber("limit"), mcp.WithBoolean("unread_only")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetMessages(c, a, tools.GetMessagesInput{Limit: intArg(r, "limit"), UnreadOnly: mcp.ParseBoolean(r, "unread_only", false)})
	})
	return s
}
func intArg(req mcp.CallToolRequest, key string) int {
	return mcp.ParseInt(req, key, 0)
}

func shortID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}
