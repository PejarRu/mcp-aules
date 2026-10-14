package main

import (
	"context"
	"encoding/json"
	"fmt"
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

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html><html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Conectar Aules</title><style>*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f4f7fb;color:#172033;font:16px system-ui,sans-serif}main{width:min(92vw,430px);padding:2rem;background:#fff;border:1px solid #dce3ed;border-radius:16px;box-shadow:0 12px 36px #17203318}h1{margin:0 0 .5rem;color:#1769aa}p{line-height:1.5}.hint{color:#5d6879;font-size:.92rem}.error{padding:.8rem 1rem;color:#8a1c1c;background:#fff0f0;border:1px solid #e8aaaa;border-radius:8px}label{display:block;margin:1rem 0 .4rem;font-weight:650}input{width:100%;padding:.8rem;border:1px solid #aeb8c6;border-radius:8px;font:inherit}input:focus{outline:3px solid #1769aa33;border-color:#1769aa}button{width:100%;margin-top:1.4rem;padding:.85rem;border:0;border-radius:8px;background:#1769aa;color:#fff;font:700 1rem system-ui;cursor:pointer}button:hover{background:#0f548b}.privacy{margin-top:1.25rem;font-size:.8rem;color:#6c7685}</style></head><body><main><h1>Conectar Aules</h1><p>Inicia sesión con tus credenciales habituales de Aules.</p>{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}<form method="post" action="/authorize"><input type="hidden" name="client_id" value="{{.ClientID}}"><input type="hidden" name="redirect_uri" value="{{.RedirectURI}}"><input type="hidden" name="state" value="{{.State}}"><input type="hidden" name="code_challenge" value="{{.Challenge}}"><input type="hidden" name="code_challenge_method" value="{{.ChallengeMethod}}"><input type="hidden" name="resource" value="{{.Resource}}"><input type="hidden" name="scope" value="{{.Scope}}"><label for="username">Usuario de Aules</label><input id="username" name="username" autocomplete="username" autocapitalize="none" spellcheck="false"><p class="hint">Usa tu nombre de usuario, no el correo electrónico.</p><label for="password">Contraseña</label><input id="password" type="password" name="password" autocomplete="current-password"><button type="submit" formnovalidate>Conectar con Aules</button></form><p class="privacy">Las credenciales se envían directamente a Aules. La contraseña no se almacena.</p></main></body></html>`))

var platformLoginTemplate = template.Must(template.New("platform-login").Parse(`<!doctype html><html lang="es"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Conectar Aules</title><style>body{max-width:430px;margin:3rem auto;padding:0 1rem;font:16px system-ui,sans-serif;color:#172033}label,input,select,button{display:block;width:100%;box-sizing:border-box;margin-top:.6rem}input,select,button{padding:.8rem;font:inherit}.error{color:#8a1c1c}</style><h1>Conectar Aules</h1>{{if .Error}}<p class="error">{{.Error}}</p>{{end}}<form method="post" action="/authorize"><input type="hidden" name="client_id" value="{{.ClientID}}"><input type="hidden" name="redirect_uri" value="{{.RedirectURI}}"><input type="hidden" name="state" value="{{.State}}"><input type="hidden" name="code_challenge" value="{{.Challenge}}"><input type="hidden" name="code_challenge_method" value="{{.ChallengeMethod}}"><input type="hidden" name="resource" value="{{.Resource}}"><input type="hidden" name="scope" value="{{.Scope}}"><label>Plataforma<select name="moodle_url">{{range .Platforms}}<option value="{{.URL}}"{{if eq .URL $.MoodleURL}} selected{{end}}>{{.Name}}</option>{{end}}</select></label><label>Usuario<input name="username" autocomplete="username"></label><label>Contraseña<input name="password" type="password" autocomplete="current-password"></label><button type="submit">Conectar con Aules</button></form>`))

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
	a := &app{cfg: cfg, store: store}
	a.mcp = mcpserver.NewStreamableHTTPServer(newMCPServer(), mcpserver.WithStateLess(true))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html lang="es"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Aules MCP</title><body><main><h1>Aules MCP</h1><p>Servidor MCP activo.</p><p>Conecta este servidor desde un cliente MCP compatible usando <code>https://anton.aules.mcp.development.antonberzins.com/mcp</code>. El inicio de sesión se abrirá durante la autorización OAuth.</p></main></body></html>`)
	})
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
	http.Redirect(w, r, target.String(), http.StatusSeeOther)
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
		mcpserver.WithInstructions("Use the read-only Aules tools to retrieve the connected user's academic information."),
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
	course("get_assignments", "Get course assignments", func(c context.Context, a *api.Client, id int) (string, error) {
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
	add(mcp.NewTool("get_calendar_events", mcp.WithDescription("Get calendar events"), mcp.WithNumber("days_ahead")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetCalendarEvents(c, a, tools.GetCalendarEventsInput{DaysAhead: intArg(r, "days_ahead")})
	})
	add(mcp.NewTool("get_upcoming_deadlines", mcp.WithDescription("Get upcoming deadlines"), mcp.WithNumber("days_ahead")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetUpcomingDeadlines(c, a, tools.GetUpcomingDeadlinesInput{DaysAhead: intArg(r, "days_ahead")})
	})
	add(mcp.NewTool("get_notifications", mcp.WithDescription("Get notifications"), mcp.WithNumber("limit"), mcp.WithBoolean("unread_only")), func(c context.Context, r mcp.CallToolRequest, a *api.Client) (string, error) {
		return tools.HandleGetNotifications(c, a, tools.GetNotificationsInput{Limit: intArg(r, "limit"), UnreadOnly: mcp.ParseBoolean(r, "unread_only", false)})
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
