# Verificación de la migración

Fecha: 5 de noviembre de 2026. Entorno: Windows, Node 24.22.1, npm 11.19.1 y Go portable 1.27.3. Frontend: Next.js 16.4.5; las versiones concretas quedan fijadas en package.json/package-lock.json. Backend: controlador modernc.org/sqlite, sin CGO.

## Backend SQLite

18 pruebas automatizadas aprobadas, 0 fallidas:

1. Integridad de SQLite, claves foráneas y WAL.
2. Idempotencia de compra y precio calculado por el servidor.
3. Dieciséis compras simultáneas de la última unidad: exactamente una confirmada.
4. Cancelación con devolución de stock una sola vez y registro de estado.
5. Fallo simulado al entregar: rollback completo del cobro; repetición sin duplicación.
6. Permisos de cliente/cocina/administrador y propiedad de pedidos.
7. Rechazo de origen ajeno y revocación de sesión al salir.
8. Registro real, contraseña con hash, login, persistencia tras reinicio y acceso demo desactivado en modo normal.
9. Cierre con total obsoleto rechazado, bloqueo de cobros posteriores y continuidad de operaciones no relacionadas.
10. Conflicto de edición de producto/stock mediante updated_at.
11. Dinero exacto, fecha de negocio de Lima y protección de fórmulas CSV.
12. Respaldo consistente y reapertura del archivo SQLite.
13. Límite de intentos de acceso.
14. Sesiones vencidas rechazadas.
15. Dos repositorios con conexiones independientes compitiendo por la última unidad.
16. Motivo de cancelación persistido en auditoría.
17. Cambio de rol aplicado a sesiones existentes y protección del administrador contra retirar su propio acceso.
18. Subida de imágenes denegada al cliente y rechazo de solicitud de formato inválido.

También se ejecutaron go vet y compilación del ejecutable. Las pruebas operan sobre archivos SQLite temporales independientes; no usan Supabase ni una base simulada en memoria.

## Frontend

Comprobación TypeScript y compilación de producción Next.js correctas. Se inspeccionaron pantallas en el navegador local.

Flujo comprobado visualmente con SQLite demo:

- Acceso de cliente y catálogo con ocho productos de ejemplo.
- Añadir un pan con palta y una observación al carrito.
- Enviar el pedido #108, confirmado en S/ 5.00 por la API.
- Acceso de administración: el pedido aparece en cocina con su observación.
- Transición a preparación y listo para recoger.
- Confirmar cobro de prueba por Yape con referencia DEMO-UI-108 y entregar.
- Reporte muestra el pedido y el importe confirmado; los cobros demo pasan de S/ 13.00 a S/ 18.00.
- Vista del reporte a 390 × 844: documento de 375 px útiles más la barra de desplazamiento; scrollWidth coincide con clientWidth. No hay desbordamiento horizontal del documento.

Durante la revisión se corrigió el envío de formularios desde botones y se hicieron visibles los errores dentro de diálogos. Los formularios mantienen validación HTML y envío por teclado. Se verificó el cobro después de corregirlo.

Capturas: vista-escritorio.jpg y vista-movil.jpg.

## Límites de la validación

No se importaron usuarios/pedidos desde una base remota: no se dispone de una exportación del Supabase anterior y el usuario solicitó sustituirlo por SQLite. El proyecto Android original permanece sin cambios.

No se probaron despliegue público, correo, pasarelas de pago, notificaciones push, dispositivo físico, restauración de archivos de imágenes ni carga sostenida de larga duración. Los registros Yape/Plin son confirmaciones manuales. La prueba de subida verifica rechazo/permisos; no es una auditoría completa de procesadores de imágenes.

La adaptación responsive se inspeccionó en un viewport de navegador, no en todos los modelos de teléfono. El control de roles es del backend; la creación inicial del administrador se realiza mediante crear-usuario.ps1.

## Repetir

Ejecutar .\verificar.ps1 desde la raíz. Para inspeccionar una base normal: desde backend ejecutar ..\.tools\go\bin\go.exe run . check. Para respaldar: .\respaldar.ps1.

## Referencias técnicas

- https://sqlite.org/pragma.html
- https://sqlite.org/foreignkeys.html
- https://pkg.go.dev/modernc.org/sqlite
- https://nextjs.org/docs/app/getting-started/installation

El esquema activo está en backend/schema.sql. No se ejecutan las migraciones PostgreSQL del proyecto de origen.

## Actualización: login por rol y personajes ficticios

21 pruebas de backend aprobadas. Se añadieron pruebas de login de los cuatro personajes por usuario y correo, rechazo de acceso por otro rol, contraseña incorrecta, repetición del alta sin restablecer contraseñas y rechazo transaccional de correos que pertenecen a otra cuenta. TypeScript, go vet y build de Next.js correctos.

Verificación visual adicional: login con usuario/contraseña de Valentina (CLIENT), Marco (COOK) y Adriana (ADMIN) completado en el navegador local. Cada cuenta mostró su identidad y el panel correspondiente. Se dejó la pantalla de login abierta y sin credenciales cargadas. Captura: vista-login.jpg.

## Actualización: CRUD del cocinero

24 pruebas de backend aprobadas; go vet, compilación Go, TypeScript y build de Next.js correctos. Las nuevas pruebas cubren alta/lectura/edición/eliminación/restauración desde COOK, rechazo de gestión desde CLIENT, conservación de pedidos históricos, devolución del stock de un producto eliminado, edición y eliminación con control de versión, auditoría e idempotencia de la eliminación.

Verificación en navegador con Marco Ramos (COOK): creación de Combo andino (prueba) a S/ 9.50 con stock 10, edición a S/ 10.00 y stock 12, eliminación, consulta en Eliminados y restauración. El producto de ejemplo permanece identificado como prueba en la base demo. Captura: vista-crud-cocina.jpg.

SQLite agrega menu_item_archives y menu_item_history mediante inicialización idempotente, con versión de esquema 2. Los permisos de cobros, avisos y recepción permanecen restringidos a ADMIN.
