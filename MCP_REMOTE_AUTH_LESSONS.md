# Lecciones reutilizables para servidores MCP remotos

## 1. Separar autenticación MCP y autenticación upstream

Son dos relaciones distintas:

1. Cliente MCP ↔ servidor MCP: OAuth 2.1, Authorization Code, PKCE S256 y bearer MCP.
2. Servidor MCP ↔ servicio upstream: OAuth, token propio, sesión o mecanismo oficial del proveedor.

El bearer MCP nunca debe ser el token upstream. El servidor debe resolver el bearer MCP a una identidad y credencial upstream cifrada.

## 2. Arquitectura multiusuario

Una URL MCP puede servir a muchas personas. Cada autorización crea una relación aislada:

```text
bearer MCP opaco → sesión de usuario → token upstream cifrado
```

Requisitos:

- Nunca guardar una credencial upstream global.
- Nunca compartir clientes API mutables entre usuarios.
- Crear o resolver cliente upstream por petición.
- Hash SHA-256 de códigos y bearer MCP en base de datos.
- Cifrar tokens upstream en reposo con AES-256-GCM o gestor de secretos equivalente.
- Códigos de autorización de un solo uso y corta duración.
- Expiración y revocación de bearer MCP.
- No registrar contraseñas, tokens, cuerpos sensibles ni parámetros de autorización completos.

## 3. OAuth compatible con ChatGPT

Flujo que funcionó:

- `GET /.well-known/oauth-authorization-server`
- `GET /.well-known/oauth-protected-resource`
- `POST /register` para Dynamic Client Registration
- `GET|POST /authorize`
- `POST /token`
- `POST /revoke`
- Authorization Code con PKCE S256
- clientes públicos con `token_endpoint_auth_method=none`

Metadata importante:

- `issuer`
- `authorization_endpoint`
- `token_endpoint`
- `registration_endpoint`
- `revocation_endpoint`
- `response_types_supported: ["code"]`
- `grant_types_supported: ["authorization_code"]`
- `code_challenge_methods_supported: ["S256"]`
- `token_endpoint_auth_methods_supported: ["none"]`
- `scopes_supported`
- `authorization_response_iss_parameter_supported: true`

Metadata de recurso:

- URL exacta del recurso `/mcp`
- servidor de autorización
- bearer mediante cabecera

Preservar durante GET → formulario POST → redirect:

- `client_id`
- `redirect_uri`
- `state`
- `code_challenge`
- `code_challenge_method`
- `resource`
- `scope`

Respuesta de autorización:

- devolver `code`
- devolver `state` sin modificar
- incluir `iss` cuando se anuncia soporte
- usar redirect `303 See Other` después del POST

Respuesta de token:

- `access_token`
- `token_type: Bearer`
- `expires_in`
- `scope`

## 4. Fallo real: CSP bloqueaba redirect a ChatGPT

Síntoma:

- Login upstream correcto.
- Código OAuth creado.
- Logs mostraban redirect al cliente.
- ChatGPT nunca llamaba `/token`.

Causa:

```http
Content-Security-Policy: default-src 'none'; form-action 'self'
```

El navegador embebido bloqueaba navegación desde el POST hacia `https://chatgpt.com`.

Corrección aplicada:

```http
Content-Security-Policy: default-src 'none'; form-action 'self' https://chatgpt.com; style-src 'unsafe-inline'
```

Lección: probar flujo en navegador real/embebido, no solo con `curl`. Una autenticación upstream correcta no demuestra que OAuth MCP haya terminado.

## 5. Diagnóstico OAuth por etapas

Añadir logs seguros para:

1. Página `/authorize` abierta.
2. Formulario enviado.
3. Autenticación upstream iniciada.
4. Autenticación upstream aceptada/rechazada.
5. Código creado.
6. Redirect iniciado.
7. `/token` recibido.
8. Canje completado/rechazado.
9. Primera petición `/mcp` autenticada.

Registrar solo IDs truncados, etapa, duración y clase de error. Nunca usuario, contraseña, código completo, verifier, bearer ni token upstream.

## 6. Stateless significa que no existe estado entre tools

Con Streamable HTTP stateless, cada llamada puede crear un cliente API nuevo. No depender de este patrón:

```text
get_site_info guarda userID → list_courses reutiliza userID
```

Cada tool debe resolver internamente identidad, configuración y contexto necesarios. Helpers como `ensureUserID` deben hidratar estado dentro de la misma petición.

## 7. Probar API real antes de diseñar tools

Proceso recomendado:

1. Obtener lista de funciones/capacidades permitidas por token.
2. Probar métodos candidatos con datos propios y sin imprimir contenido privado.
3. Registrar solo disponibilidad, campos, conteos y códigos de error.
4. Comparar respuesta raw con structs internos.
5. Detectar campos descartados, truncados o reinterpretados.
6. Crear contrato MCP después de conocer respuesta real.

Errores detectados con este método:

- Descripciones truncadas localmente, no por Moodle.
- Notificaciones llamadas con parámetros incompatibles.
- Campos válidos descartados por structs demasiado pequeños.
- Resúmenes de notas vacíos pese a existir cientos de grade items.
- Timestamps Unix mostrados en UTC por contenedor.

## 8. Datos completos bajo demanda

Separar tools de listado y detalle:

- listado: IDs, nombres, estados y resumen corto;
- detalle: texto completo, HTML, adjuntos, feedback y metadata.

No truncar campos en tool de detalle. Devolver HTML original y texto limpio. Mantener IDs estables para consultas posteriores.

## 9. Timestamps y timezone

Timestamps Unix representan instantes UTC. No formatearlos con timezone del contenedor.

Respuesta recomendada:

```json
{
  "due_at": "2026-10-13T23:59:00+02:00",
  "due_at_utc": "2026-10-13T21:59:00Z",
  "due_timestamp": 1791928740,
  "timezone": "Europe/Madrid"
}
```

Preferir timezone IANA por usuario. Si upstream no la expone, usar valor configurable y documentar fallback. Nunca usar offset fijo porque rompe horario de verano.

## 10. URLs y secretos

- Eliminar `token`, `wstoken`, firmas y credenciales de URLs devueltas al modelo.
- No descargar archivos al servidor salvo requisito explícito.
- Validar redirect URIs por coincidencia exacta.
- HTTPS obligatorio, excepto localhost para clientes nativos.
- Limitar tamaño de bodies y aplicar timeouts HTTP.
- Respuestas de error públicas deben ser genéricas; detalles quedan en logs seguros.

## 11. Read-only por defecto

Registrar únicamente tools de lectura hasta tener autorización explícita, controles de confirmación y modelo de permisos claro. No exponer acciones destructivas o sociales sensibles solo porque upstream las permita.

Para productos de citas, acciones como swipe, match, mensaje, edición de perfil, geolocalización y carga de fotos tienen impacto humano directo. Deben quedar fuera del primer alcance.

## 12. APIs privadas y términos del proveedor

Antes de implementar integración con Hinge o Tinder:

- Confirmar si existe API oficial para el caso de uso.
- Revisar autenticación admitida, términos, límites y políticas anti-automatización.
- No usar endpoints privados de aplicaciones móviles, ingeniería inversa, cookies capturadas, emulación de dispositivo ni evasión de controles.
- No almacenar mensajes, fotos, ubicación u otros datos de terceros sin necesidad clara y política de retención.
- Diseñar alternativa de importación/exportación manual si API oficial no permite acceso.

## 13. Checklist de producción

- OAuth discovery y protected-resource metadata correctos.
- DCR y PKCE S256 probados con cliente real.
- `state`, `resource`, `scope` e `iss` preservados.
- CSP permite redirect esperado sin abrir destinos innecesarios.
- Tokens upstream cifrados; bearer MCP hasheados.
- Aislamiento multiusuario probado.
- MCP stateless sin estado global por usuario.
- Tools read-only y contratos explícitos.
- URLs saneadas.
- Fechas RFC 3339 con timezone.
- Tests unitarios, race detector, lint/vet y build.
- Prueba real con datos propios, sin exponer datos personales en logs.
- Health check público; `/mcp` devuelve 401 sin bearer.
- Proxy, TLS, volúmenes y nombres Compose aislados de otros servicios.
