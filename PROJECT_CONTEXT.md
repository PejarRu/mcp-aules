# Contexto de continuidad

Este documento es el punto de entrada para una persona o agente que retome
`mcp-aules`. Describe el estado real a 2026-10-14. Leer junto con `README.md`

## Objetivo y alcance

`mcp-aules` es un servidor remoto MCP multiusuario que permite a clientes como
ChatGPT consultar datos propios de Aules. Usa Streamable HTTP stateless y OAuth
2.1 propio para el cliente MCP. El usuario entrega sus credenciales de Aules en
una página HTTPS del servidor; se obtiene un token Moodle y nunca se persiste la
contraseña.

Alcance actual:

- Solo lectura: perfil, cursos, contenidos, notas, tareas, recursos, calendario,
  plazos y notificaciones.
- Plataformas soportadas: Semipresencial (`/semipresencial`) y Formación
  Profesional (`/fp`), seleccionadas durante cada autorización.
- Aislamiento por usuario y por sesión. No existe un token Aules global.

Fuera de alcance actual:

- Mutar cursos, tareas, perfil o cualquier dato Aules.
- Descargar archivos al servidor.
- Compartir sesiones o usar estado mutable entre llamadas MCP.

## Estado de producción

| Elemento | Valor |
|---|---|
| Repositorio | `https://github.com/PejarRu/mcp-aules` |
| URL MCP canónica | `https://anton.aules.mcp.development.antonberzins.com/mcp` |
| URL base informativa | `https://anton.aules.mcp.development.antonberzins.com/` |
| Servidor | `oracle-vps` |
| Directorio desplegado | `/opt/mcp-aules/app` |
| Compose | `/opt/mcp-aules/app/compose.yaml` |
| Contenedor | `mcp-aules-moodle-mcp-1` |
| Persistencia | volumen `/data/moodle-mcp.db` dentro del contenedor |
| Proxy | Traefik, red externa `traefik` |

La rama desplegable es `main`. El remoto del VPS usa HTTPS para leer el
repositorio público; no necesita una clave SSH privada en el servidor.

Despliegue normal, sin tocar `.env` ni el volumen de datos:

```sh
git push origin main
ssh oracle-vps 'git -C /opt/mcp-aules/app pull --ff-only origin main && sudo docker compose -f /opt/mcp-aules/app/compose.yaml up -d --build'
```

Comprobaciones posteriores:

```sh
curl -i https://anton.aules.mcp.development.antonberzins.com/health
curl -i https://anton.aules.mcp.development.antonberzins.com/mcp
ssh oracle-vps 'sudo docker logs --since 10m mcp-aules-moodle-mcp-1'
```

Se espera `204` en `/health` y `401` en `/mcp` sin bearer. Nunca imprimir
variables de `.env`, tokens, contraseñas, códigos OAuth o contenido privado en
logs o conversaciones.

## Arquitectura relevante

```text
Cliente MCP -- OAuth 2.1 + PKCE --> mcp-aules -- token Moodle --> Aules
                 bearer MCP opaco       SQLite cifrado
```

- `cmd/moodle-mcp/main.go`: rutas HTTP, OAuth, validación bearer, selector de
  plataforma y registro MCP.
- `internal/oauth/store.go`: SQLite, hash SHA-256 de códigos y bearers, tokens
  upstream cifrados AES-256-GCM, expiración/revocación y migraciones.
- `internal/config/config.go`: lista cerrada de plataformas Aules permitidas.
- `internal/api/`: cliente Moodle por petición; el token se obtiene de
  `<plataforma>/login/token.php` para ambas plataformas.
- `internal/tools/`: tools read-only y sus contratos.

El bearer MCP no es el token Moodle. Cada llamada resuelve la sesión y crea el
cliente upstream que necesita; no confiar en estado guardado por una tool previa.

## OAuth y ChatGPT

Las rutas públicas son:

- `POST /mcp`: único endpoint MCP.
- `GET /.well-known/oauth-authorization-server`: metadata OAuth.
- `GET /.well-known/oauth-protected-resource`: metadata con recurso exacto
  `/mcp`.
- `POST /register`: Dynamic Client Registration.
- `GET|POST /authorize`, `POST /token`, `POST /revoke`.

El flujo soportado es Authorization Code con PKCE S256 y clientes públicos. Se
preservan `state`, `resource`, `scope`, challenge y método entre formulario y
callback; se devuelve `iss`; el token response contiene el `resource` validado.
Los códigos duran cinco minutos, son de un solo uso, y los bearer MCP duran una
hora.

Para configurar ChatGPT hay que usar **exactamente** la URL canónica terminada
en `/mcp`. Usar solo el dominio permite que OAuth llegue a completarse, pero
ChatGPT descubre acciones en la raíz y no llegará al handler MCP. Este fue un
incidente real y ya se mitigó: `GET /` avisa claramente y un `POST /` devuelve:

```json
{
  "error": "invalid_mcp_endpoint",
  "mcp_endpoint": "https://anton.aules.mcp.development.antonberzins.com/mcp"
}
```

Si ChatGPT muestra `Authentication succeeded, action discovery failed`, primero
confirmar la URL configurada. Después revisar logs por estas fases, sin datos
sensibles: página authorize, autenticación Aules, callback, token exchange y
petición bearer a `/mcp`.

## Hitos y decisiones

| Commit | Resultado |
|---|---|
| `11fb9e2` | Selector por sesión para Semipresencial y FP; migración segura de plataforma en SQLite. |
| `dea7f39` | Logs seguros de autenticación MCP. |
| `cc89756` | CORS limitado a dominios ChatGPT y preflight para `/mcp`. |
| `0d23976` | Persistencia y validación de audiencia OAuth `resource`. |
| `5e1b4a7` | Metadata de tools para clientes ChatGPT modernos. |
| `9d104ae` | Explicación y error accionable cuando se usa la URL base en vez de `/mcp`. |

Las correcciones de CORS, `resource` y metadata se conservaron por
interoperabilidad. No fueron la causa del incidente de discovery: la causa fue
configurar la URL sin `/mcp`.

## Ideas descartadas y por qué

- Usar `https://aules.edu.gva.es/fp/login/index.php?loginredirect=1` como API
  de autenticación: descartado. Es una página HTML y no devuelve el token Moodle
  JSON que espera el cliente. FP y Semipresencial usan `login/token.php`.
- Permitir cualquier valor en `MOODLE_URL`: descartado por riesgo SSRF y por no
  limitar el servidor a proveedores autorizados. Solo se aceptan las dos bases
  Aules conocidas.
- Resolver identidad de usuario en una tool y reutilizarla en otra: descartado;
  Streamable HTTP es stateless.
- Diagnosticar el discovery solo por la alerta de ChatGPT: insuficiente. Hubo que
  correlacionar OAuth y las peticiones reales a `/mcp`.

## Riesgos y trabajo pendiente

1. Hacer una prueba funcional completa de tools con una cuenta FP y otra
   Semipresencial, sin registrar datos privados. La conexión OAuth está validada;
   aún conviene validar respuestas Moodle representativas por plataforma.
2. Revisar el contrato de output de cada tool con MCP Inspector o API Playground
   antes de añadir nuevas tools. Mantener listados pequeños y detalle separado.
3. Mantener pruebas de aislamiento entre usuarios, expiración y revocación si se
   modifica `internal/oauth/store.go`.
4. Añadir documentación de usuario final si se publica fuera de ChatGPT:
   URL `/mcp`, permisos de datos, retención y proceso de revocación.
5. No cambiar `AULES_ENCRYPTION_KEY`: invalida el descifrado de sesiones ya
   almacenadas.

## Flujo de cambio

1. Leer este archivo y los tests próximos al código que se editará.
2. Aplicar el cambio mínimo. Las rutas OAuth y el esquema SQLite son sensibles.
3. Ejecutar `gofmt`, `go test -race ./...`, `go vet ./...` y
   `go build ./cmd/moodle-mcp`.
4. Hacer commit, push y desplegar por el procedimiento anterior.
5. Verificar health, respuesta 401 de `/mcp`, logs del contenedor y un cliente
   MCP real si se modificó protocolo/autenticación.


## Planificación diaria y estado de entregas

La necesidad prioritaria es que ChatGPT no recomiende repetir tareas ya enviadas. `get_assignments(course_id)` se amplía para llamar `mod_assign_get_submission_status` por assignment e incluir estado individual. Para plan general: consultar `list_courses`, luego `get_assignments` para todos los cursos activos pertinentes. `submitted` y `submitted_late` cuentan como entregadas, aunque sigan sin nota; `draft` no se ha enviado definitivamente; `not_submitted` indica que Moodle no registra entrega; `status_unavailable` se trata como desconocido, nunca como pendiente. Fecha vencida sola no acredita falta de entrega.

Coste operativo: una petición REST adicional por tarea. Revisar rendimiento con cursos grandes y no cambiar a paralelismo hasta medir. Tests deben cubrir enviada tarde (no overdue), borrador, sin entrega y fallo API (unknown).

## Reautenticación móvil ChatGPT (aplazado)

En móvil, Aules puede autenticar y la app ChatGPT abrir una sesión vacía mientras el chat original retiene un popup OAuth antiguo. El usuario confirmó que reenviar el prompt en el chat original hizo que MCP funcionara; el popup era estado visual obsoleto del cliente. Se añadió handoff móvil (`73cdde2`), pero eso no puede garantizar restaurar el chat original. No seguir cambiándolo ahora; investigar solo con logs nuevos por etapas authorize/upstream/callback/token/MCP. No implementar cookies o deep links inventados.
