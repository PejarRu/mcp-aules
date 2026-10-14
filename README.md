# Aules MCP Server

Servidor remoto Model Context Protocol para consultar Aules mediante OAuth 2.1. Cada usuario inicia sesión en Aules desde el navegador; su token Aules se cifra en SQLite y recibe un bearer MCP independiente.

## Endpoints

- `POST /mcp`: Streamable HTTP stateless, protegido por bearer.
- `GET /health`: health check (`204`).
- `GET /.well-known/oauth-authorization-server`: discovery OAuth.
- `GET /.well-known/oauth-protected-resource`: metadata del recurso MCP.
- `POST /register`: Dynamic Client Registration para clientes públicos.
- `GET|POST /authorize`: Authorization Code y login web Aules.
- `POST /token`: canje de código con PKCE S256.
- `POST /revoke`: revocación de bearer.

Solo se publican tools de lectura: perfil, cursos, contenidos, notas, tareas, recursos, calendario, plazos y notificaciones. No se publican login MCP, descargas ni operaciones mutadoras.

Tools enriquecidas: `get_assignments`, `get_assignment_details`, `get_assignment_status`, `list_quizzes`, `get_quiz_status`, `get_grades`, `get_course_gradebook`, `list_course_announcements`, `get_course_learning_content`; `get_notifications` devuelve cuerpo completo y contexto.

## Configuración

| Variable | Uso |
|---|---|
| `PUBLIC_BASE_URL` | URL HTTPS pública, sin `/` final |
| `AULES_ENCRYPTION_KEY` | Clave AES-256: 32 bytes codificados en base64 |
| `LISTEN_ADDR` | Dirección HTTP; default `:8080` |
| `DATABASE_PATH` | SQLite; default `/data/moodle-mcp.db` |
| `MOODLE_URL` | Plataforma predeterminada: `https://aules.edu.gva.es/semipresencial` o `https://aules.edu.gva.es/fp` |

Generar clave:

```sh
openssl rand -base64 32
```

No reutilizar ni publicar la clave. Perderla impide descifrar sesiones existentes.

## Desarrollo

Requiere Go 1.26.

```sh
go test -race ./...
go vet ./...
go build ./cmd/moodle-mcp
```

Ejecución local HTTPS requiere proxy inverso y variables anteriores:

```sh
go run ./cmd/moodle-mcp
```

## Docker y Traefik

`compose.yaml` usa red externa `traefik`, volumen persistente y dominio `anton.aules.mcp.development.antonberzins.com`.

```sh
cp .env.example .env
# Sustituir AULES_ENCRYPTION_KEY
docker compose up -d --build
```

Traefik debe tener red `traefik`, entrypoint `websecure` y gestión TLS configurados. El contenedor corre sin root y no publica puerto directamente.

## Flujo OAuth

1. Cliente obtiene metadata y se registra con `redirect_uris` HTTPS (localhost HTTP permitido para clientes nativos).
2. Cliente abre `/authorize` con `response_type=code`, `client_id`, `redirect_uri`, `state`, `code_challenge` y `code_challenge_method=S256`.
3. Usuario introduce credenciales Aules en formulario HTTPS. Credenciales no se persisten.
4. Código dura 5 minutos y tiene un solo uso. `/token` exige `grant_type=authorization_code` y `code_verifier`.
5. Bearer MCP dura 1 hora. Token Aules permanece cifrado en SQLite con AES-256-GCM.
6. `/revoke` invalida bearer MCP.
