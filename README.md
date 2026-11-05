# Desayunos · Next.js + Go + SQLite

Migración web del proyecto Android. Aplicación local para cliente, cocina y administración, con interfaz adaptable a móvil y escritorio.

**La implementación actual usa SQLite. No requiere Supabase, claves API ni un servidor de base de datos.** El análisis inicial conserva el diagnóstico del origen; la decisión posterior del usuario de usar SQLite reemplaza su propuesta de arquitectura.

## Abrir la vista de prueba

Desde PowerShell, en esta carpeta:

```powershell
.\iniciar.ps1 -Demo
```

Abre http://127.0.0.1:3000. Selecciona Cliente, Usuario o Admin e ingresa con las credenciales de docs/PERSONAJES.md. Los ejemplos persisten en `.data/demo.db`. El modo demo usa el mismo backend transaccional SQLite que el modo normal, con cuentas y productos de prueba separados.

La ejecución de revisión ya está disponible en esa dirección mientras los procesos de esta sesión estén activos. No inicies otra instancia en los mismos puertos.

## Usar tus propias cuentas y productos

1. Crea el primer administrador. La contraseña se solicita de forma oculta y no se pasa como argumento:

```powershell
.\crear-usuario.ps1 -Email 'tu@correo.com' -Nombre 'Tu nombre' -Telefono '999123456'
```

2. Inicia el modo normal:

```powershell
.\iniciar.ps1
```

3. Entra como personal y agrega tus productos desde **Carta y stock**. El registro público crea exclusivamente clientes. Puedes crear credenciales propias; las cuentas ficticias solicitadas se detallan en docs/PERSONAJES.md.

Para crear personal de cocina:

```powershell
.\crear-usuario.ps1 -Email 'cocina@correo.com' -Nombre 'Cocina' -Telefono '999123457' -Rol COOK
```

El modo normal utiliza `.data/desayunos.db`, separado del entorno demo. Las contraseñas y usuarios antiguos de Supabase no se importan automáticamente: el origen local contiene el código y las migraciones, pero no una exportación de datos reales.

## Funciones

- Registro e inicio de sesión, perfil, sesión persistente y cierre con revocación.
- Roles CLIENT, COOK y ADMIN comprobados por el backend en cada solicitud.
- Carta por categorías, búsqueda, imágenes, disponibilidad y stock.
- Carrito con notas, precios confirmados por servidor y envío idempotente recuperable.
- Historial propio, comanda, filtros, búsqueda y cancelación antes de preparar.
- Preparación, pedido listo y entrega; se exige pago antes de entregar.
- Cobros manuales en efectivo, Yape y Plin. Los digitales requieren referencia.
- Cobro y entrega en una sola transacción; un fallo revierte la operación completa.
- Alta y edición de productos, ajustes de stock y protección contra ediciones desactualizadas.
- Imágenes locales JPG/PNG/WebP de hasta 5 MB y URL HTTPS opcional.
- Avisos, apertura/cierre de recepción y centro de notificaciones con marcas de lectura por usuario/dispositivo.
- Reporte diario en soles, pagos, productos, pendientes y exportación CSV protegida contra fórmulas.
- Caja conciliada por jornada de Lima, cierre único y bloqueo de nuevos cobros de esa jornada.
- Auditoría persistente de estados, motivos de cancelación y movimientos de stock.
- Administración de roles mediante API restringida y creación inicial mediante CLI.

Los pagos son registros manuales: no se realiza una transferencia a Yape/Plin ni se consulta una pasarela. Los avisos se sincronizan cada 10 segundos mientras la página está visible; no hay push con el navegador cerrado.

## Estructura

| Ruta | Uso |
| --- | --- |
| `frontend/` | React, Next.js App Router, TypeScript, estilos e ilustraciones locales. |
| `backend/` | API HTTP Go, dominio, SQLite, autenticación y pruebas. |
| `backend/schema.sql` | Esquema SQLite embebido, aplicado automáticamente al iniciar. |
| `database/migrations/001_sqlite.sql` | Copia de referencia del esquema SQLite. |
| `.data/desayunos.db` | Datos del modo normal. |
| `.data/demo.db` | Datos independientes de demostración. |
| `.data/uploads/` | Imágenes subidas. Conservarlas junto con los respaldos. |
| `.tools/go/` | Go portable instalado desde go.dev con SHA-256 verificado. |
| `docs/VERIFICACION.md` | Evidencias y límites de verificación. |

## Ejecutar y verificar

Node y npm ya estaban instalados. El proyecto incluye Go portable y las dependencias del frontend instaladas en este equipo. En una máquina nueva usa Node 20.9+ y Go 1.26+; ejecuta `npm ci` en frontend y `go mod download` en backend.

```powershell
.\verificar.ps1
.\iniciar.ps1 -Produccion
```

`iniciar.ps1` compila Go, inicia la API sin abrir una ventana extra y ejecuta Next.js en la consola actual. Ctrl+C detiene la ejecución. Ambos servicios escuchan en loopback por defecto.

Ejecución manual en dos terminales:

```powershell
# Terminal 1: backend
cd backend
..\.tools\go\bin\go.exe run .

# Terminal 2: frontend
cd frontend
npm.cmd run dev
```

La configuración de ejemplo está en `.env.example`. Las variables del backend se proporcionan al proceso; Go no carga archivos `.env` automáticamente. Los scripts raíz configuran las rutas y el modo. Next.js reenvía `/api/*` al backend para mantener el mismo origen.

Variables: `APP_MODE=sqlite|demo`, `APP_ORIGIN`, `HTTP_ADDR` o `PORT`, `DATA_DIR`, `DATABASE_PATH`, `COOKIE_SECURE`. Frontend: `API_URL`. En HTTPS usa `COOKIE_SECURE=true` y configura el origen público exacto. La API valida Origin en las mutaciones y limita intentos de autenticación. No expongas el modo demo como si fuese un entorno privado con cuentas reales.

## SQLite, respaldo y recuperación

Las tablas normalizadas guardan importes en céntimos enteros, relaciones con claves foráneas, índices y restricciones. WAL, busy_timeout y transacciones IMMEDIATE controlan las escrituras concurrentes. La creación de pedido incluye reserva de stock y recibo idempotente; cobro y entrega comparten transacción. El número de pedido se asigna dentro de la misma transacción.

```powershell
.\respaldar.ps1
.\respaldar.ps1 -Demo
cd backend
..\.tools\go\bin\go.exe run . check
```

El respaldo usa `VACUUM INTO`, incluso con la base en uso. Para restaurar, detén la aplicación, conserva una copia del estado anterior y coloca el respaldo en la ruta DATABASE_PATH de una carpeta de datos limpia. Copia también `uploads/`. Evita copiar solamente el archivo principal de una base abierta en WAL, porque puede haber cambios en los archivos `-wal` y `-shm`.

## Alcance y límites

La migración conserva el flujo de retiro en local del Android. Widget Android, notificaciones push con la app cerrada, recetas de ingredientes, sedes, reembolsos, reapertura de caja y recuperación por correo no forman parte de esta entrega. El registro local no verifica correo. El inventario cuenta productos vendibles, no recetas ni componentes de combos.

El repositorio SQLite actual carga una instantánea del negocio en cada operación y guarda los cambios en una transacción. Es adecuado para la operación local de un negocio pequeño; antes de un historial grande conviene implementar paginación y consultas agregadas por jornada. La sesión dura siete días, se almacena mediante token aleatorio con hash en SQLite y puede revocarse al cerrar sesión; no utiliza refresh tokens externos. Las contraseñas usan PBKDF2-HMAC-SHA256, sal individual y 600000 iteraciones.

La aplicación Android de origen no fue modificada y no comparte automáticamente datos con esta base SQLite.

## Acceso con personajes ficticios

El login tiene las opciones **Cliente**, **Usuario** (cocina) y **Admin**. Acepta usuario corto o correo y contraseña. Los cuatro personajes, sus credenciales y cómo volver a crearlos están en [docs/PERSONAJES.md](docs/PERSONAJES.md). Estas cuentas se crearon explícitamente a pedido del usuario, en ambas bases locales. No se generan automáticamente al iniciar la aplicación.

## CRUD de desayunos para cocina

El panel de usuario/cocinero incluye **Desayunos**. COOK y ADMIN pueden crear, consultar, editar y eliminar desayunos, gestionar sus imágenes, precios, categorías y stock. Los clientes conservan acceso de lectura a la carta disponible.

La eliminación retira el desayuno de la carta y lo mueve a **Eliminados**, donde puede restaurarse. Conserva pedidos, precios históricos, stock y auditoría; una cancelación posterior de un pedido devuelve la reserva aunque el producto esté eliminado. Las ediciones y eliminaciones verifican la versión para rechazar vistas desactualizadas.

Los controles de recepción, publicación de avisos, cobros y caja siguen requiriendo administración. Esta ampliación de cocina se limita al CRUD de desayunos y sus imágenes.
