> DECISIÓN POSTERIOR DEL USUARIO: la implementación final usa SQLite y autenticación local en Go, sin Supabase. Este documento conserva el análisis histórico del origen. Consultar README.md y docs/VERIFICACION.md para el resultado actual.

# Análisis previo a la migración de Desayunos

Fecha: 5 de noviembre de 2026.
Origen: D:\Desayunos-Android
Destino solicitado: D:\desayuno_migracion
Tecnologías solicitadas: React con Next.js para frontend y Go para backend.

## 1. Resultado de la revisión

El proyecto es una aplicación Android nativa en Kotlin y Jetpack Compose, con tres roles: CLIENT, COOK y ADMIN. No existe frontend React ni servidor Go en el origen. La migración requiere construir una interfaz web y una API nuevas, conservando los contratos de datos y las reglas de negocio existentes.

El backend actual comprende Supabase Auth, PostgreSQL con RLS y funciones transaccionales, y Storage para imágenes. Las operaciones críticas no están solamente en Kotlin: buena parte está implementada en SQL. Se propone conservar inicialmente esa base e incorporar Go como API de la aplicación, evitando duplicar las reglas transaccionales en dos implementaciones.

Esta entrega corresponde al análisis previo solicitado. No constituye una aplicación migrada ni una migración de datos ejecutada.

## 2. Evidencias y estructura del origen

| Ubicación | Contenido y relevancia |
| --- | --- |
| app/src/main/java/com/example/desayuno/actividades/ActividadPrincipal.kt | Entrada real de la aplicación; carga Compose y destinos de notificaciones. |
| ui/DesayunoApp.kt | Tema claro/oscuro y selección de interfaz según rol del servidor. |
| ui/AuthScreen.kt | Acceso de cliente y personal, registro y validaciones. |
| ui/ClientScreens.kt | Carta, categorías, búsqueda, carrito, observaciones, historial y cancelación. |
| ui/CookScreens.kt | Cola operativa, filtros, búsqueda, estados, carta, disponibilidad y edición. |
| ui/ManagementControls.kt | Perfil, cancelaciones y controles de cobro/entrega por rol. |
| ui/ReportScreens.kt y model/SalesReport.kt | Jornada de Lima, pagos, métricas, productos, CSV y cierre. |
| ui/NotificationScreens.kt y notification/ | Centro de avisos, lecturas locales y notificaciones Android. |
| ui/DesayunoViewModel.kt | Coordinación, sesión, sincronización y persistencia de un intento de compra pendiente. |
| repository/DesayunoRepository.kt | Estado de datos y publicación de instantáneas confirmadas. |
| network/SupabaseClient.kt | Transporte HTTP, Auth, renovación, paginación, RPC y subida de imágenes. |
| supabase/migrations/ | Cuatro migraciones para reglas de negocio, Storage, caja y entrega atómica. |
| supabase/tests/ y app/src/test/ | Pruebas de SQL, acceso, pagos, reportes, avisos y aprovisionamiento. |
| app/src/androidTest/ | Pruebas de interfaz responsive y notificaciones. |
| docs/historias_usuario.json | 30 historias con criterios de aceptación y estado declarado. |
| tmp/ui-* | Capturas locales de referencia visual; se inspeccionaron login y cobro con Yape. |
| output/ | Entregables históricos: APK 1.1.0 y PDF de historias. |

Las rutas ui/, model/, network/, repository/ y notification/ de la tabla son relativas a app/src/main/java/com/example/desayuno/.

Existen layouts XML heredados. La actividad de entrada usa Compose; migrar los XML como referencia principal dejaría fuera funcionalidades actuales. build.gradle declara versión 1.2.0, mientras el APK conservado en output es 1.1.0. El APK histórico no demuestra el comportamiento de todo el código actual.

## 3. Funcionalidad que debe conservarse

| Área | Comportamiento encontrado | Equivalente web propuesto |
| --- | --- | --- |
| Acceso | Correo/contraseña, confirmación de correo cuando Auth la exige, recuperación de sesión y perfil real. | Login/registro y sesión administrada por el servidor. |
| Roles | CLIENT, COOK y ADMIN leídos de profiles; el selector de acceso no concede permisos. | Rutas y acciones por rol con autorización en Go y base de datos. |
| Carta | Bebidas, panes, extras y combos; nombre, descripción, precio, imagen y stock. | Catálogo responsive con búsqueda y filtros. |
| Carrito | Cantidades, notas y estimación del importe. | Resumen de compra visible y confirmación del precio del servidor. |
| Pedidos | Creación transaccional, número del servidor e identidad por customer_id. | Checkout e historial propios. |
| Reintentos | request_id persistido y carrito bloqueado ante resultado incierto. | Persistencia del mismo intento y reenvío sin duplicación. |
| Cocina | Filtros por estado, por cobrar, completados y búsqueda por número/contacto. | Tablero de cocina con comandas y acciones claras. |
| Administración | Editar carta, stock, imágenes, avisos y apertura de recepción. | Panel de gestión separado de la vista de cliente. |
| Cobros | Efectivo, Yape y Plin; referencia para pagos digitales; confirmación manual. | Modal de cobro con importe del servidor y referencia. |
| Entrega | Pago y entrega atómicos por ADMIN; COOK entrega si ya está pagado. | Acción principal según estado y permiso. |
| Reportes | Ingresos por fecha de pago, unidades, productos, pendientes y CSV. | Reportes por jornada, filtros y exportación desde backend. |
| Caja | Cierre único por jornada conciliada; impide cobros posteriores de ese día. | Vista de caja con confirmación del total vigente. |
| Avisos | Anuncios y último estado por pedido, lecturas guardadas en el teléfono. | Centro de notificaciones y enlace al pedido. |
| Perfil | Edición de nombre y teléfono propios. | Ajustes de perfil. |

La cocina también puede consultar reportes en la interfaz actual. Cerrar caja, cobrar, editar carta y publicar avisos son acciones de ADMIN. La matriz de permisos debe preservar esta diferencia.

## 4. Datos y reglas transaccionales

Entidades principales: profiles, menu_items, orders, order_items, order_status_history, product_stock_movements, payments, business_settings y daily_cash_closures. stock_inventory es una estructura histórica conservada, sin acceso de negocio desde la aplicación actual.

Funciones existentes a conservar inicialmente:

- create_order: valida sesión/contacto, carrito, disponibilidad y cantidades; consolida líneas; serializa reintentos; bloquea productos en orden estable; calcula precios en servidor; registra pedido, detalles, movimientos e historial.
- transition_order: valida propiedad/rol y transición, exige pago para entregar y devuelve reservas una sola vez al cancelar.
- complete_order: cobra y entrega en una transacción, verifica permisos y admite repetición sin duplicar el cobro.
- save_menu_item: escritura de ADMIN, movimientos de stock y control de concurrencia con updated_at.
- record_payment: un pago íntegro por pedido, importe calculado en base, método y referencia verificada por personal.
- publish_announcement y set_accepting_orders: operaciones restringidas a ADMIN.
- close_cash_day: concilia el total con bloqueo por jornada compartido con los cobros.

Flujo de estados: PENDING -> PREPARING -> READY -> DELIVERED. La cancelación pasa de PENDING a CANCELLED, requiere motivo y ausencia de pago. Un pedido pagado no se cancela por este flujo.

El stock corresponde a productos vendibles. Un combo tiene stock propio; no descuenta automáticamente sus componentes. No hay inventario de ingredientes por recetas implementado.

Los reportes usan America/Lima y soles peruanos. La base utiliza numeric para importes; los modelos Kotlin aún usan Double en presentación. La API nueva debe definir importes exactos, por ejemplo céntimos enteros, con conversión explícita al numeric existente. No trasladar cálculos monetarios a float64.

## 5. Hallazgos y límites relevantes

1. docs/PLAN_PROYECTO.md es un diagnóstico histórico. Problemas allí descritos, como login ficticio, datos de ejemplo y pedidos solo locales, ya cuentan con correcciones en el código actual. Debe prevalecer la implementación revisada.
2. No se verificó el Supabase remoto. Los SQL y las pruebas locales no demuestran qué migraciones están aplicadas ni la integridad de los datos reales.
3. Las migraciones 001-003 alteran un esquema previo; no son un instalador completo para una base vacía. tests/bootstrap.sql es exclusivamente una base falsa de pruebas y no debe usarse como esquema productivo. La migración web deberá incluir una ruta de instalación desde cero si se necesita ese escenario.
4. Los paquetes COMPLETO/V3 y recuperación V4 no sustituyen la revisión de la migración 004 de complete_order. Inventariar lo aplicado antes de ejecutar SQL.
5. Pedidos históricos pueden tener customer_id NULL. No adjudicarlos por coincidencias de nombre o teléfono.
6. La sincronización actual es sondeo cada 10 segundos en primer plano. No hay push fiable con la aplicación cerrada. El widget Android requiere un equivalente específico si se desea conservar esa experiencia en web.
7. El centro muestra una actualización actual por pedido y anuncios; no equivale al historial completo de auditoría. Las marcas de lectura son locales al dispositivo.
8. El transporte pagina lecturas, pero el repositorio termina cargando el historial accesible completo. En web conviene filtrar/paginar en Go y agregar reportes en servidor para evitar recargar todo cada 10 segundos.
9. La respuesta create_order/transition_order contiene la cabecera; complete_order sí devuelve detalles y pago. La API Go debe ofrecer DTO consistentes o una lectura posterior para no mostrar comandas incompletas tras una escritura.
10. No hay integración automática con proveedores Yape/Plin, reembolsos, reapertura de caja, recuperación completa de contraseña, sedes o fidelización implementados en estos flujos.
11. Las historias HU-23 y HU-29 son propuestas; HU-25 y HU-30 son parciales. No presentarlas como funciones terminadas al migrar.
12. Los scripts de reparación de Auth y de cuentas son herramientas administrativas; no deben ejecutarse automáticamente al arrancar Go ni convertirse en endpoints públicos.

## 6. Arquitectura propuesta para la implementación

Navegador -> frontend React/Next.js -> API Go -> Supabase Auth/PostgREST/RPC/Storage -> PostgreSQL.

Next.js se ocupará de páginas, navegación, composición visual y estados de interfaz. Go será la API de aplicación: sesión, autorización, validación del contrato, errores uniformes, paginación, reportes y coordinación de operaciones. Las transacciones críticas se conservarán en las RPC existentes durante la primera etapa.

Para mantener RLS y auth.uid(), Go deberá realizar las llamadas de negocio con el JWT del usuario. No utilizar una clave administrativa global como sustituto de la identidad del usuario. Si posteriormente se adopta conexión SQL directa, habrá que diseñar y probar explícitamente el contexto de identidad y permisos de cada transacción.

Diseño de sesión propuesto: cookies HttpOnly, Secure en despliegue HTTPS, SameSite y protección CSRF para operaciones mutables. Coordinar renovación y cierre de sesión sin exponer refresh tokens al almacenamiento accesible por JavaScript. Configurar el acceso desde el mismo origen mediante un proxy /api para simplificar el despliegue.

Estructura propuesta, todavía no generada:

    desayuno_migracion/
      frontend/              # Next.js, React y TypeScript
      backend/
        cmd/api/
        internal/auth/
        internal/catalog/
        internal/orders/
        internal/payments/
        internal/reports/
        internal/supabase/
      database/migrations/   # Esquema y cambios versionados, con procedencia
      docs/
      .env.example
      README.md

API inicial propuesta:

| Operación | Ruta orientativa | Contrato existente |
| --- | --- | --- |
| Acceso/sesión/perfil | /api/v1/auth/* y /api/v1/me | Auth + profiles |
| Carta | GET /api/v1/menu | menu_items con RLS |
| Guardar producto | PUT /api/v1/menu/{id} | save_menu_item con versión esperada |
| Imágenes | POST /api/v1/images | Storage, ADMIN y límite de 5 MB |
| Pedidos | GET/POST /api/v1/orders | Lectura paginada y create_order con request_id |
| Estado | POST /api/v1/orders/{id}/status | transition_order |
| Cobro | POST /api/v1/orders/{id}/payment | record_payment |
| Completar | POST /api/v1/orders/{id}/complete | complete_order |
| Avisos | GET/POST /api/v1/announcements | Lectura y publish_announcement |
| Recepción | GET/PATCH /api/v1/business-settings | business_settings y set_accepting_orders |
| Reporte/CSV | GET /api/v1/reports/daily | Agregación autorizada por jornada |
| Cierre de caja | POST /api/v1/cash-closures | close_cash_day |

Las rutas son una propuesta; no existen todavía. Los endpoints deben expresar autorización real y errores distinguibles de sesión, permisos, stock y conflicto de edición.

## 7. Dirección visual propuesta

Conservar la identidad de desayuno local: fondos crema, verde profundo, acentos terracota y tarjetas limpias. Jerarquía tipográfica clara, precios visibles y fotografía real del catálogo cuando esté disponible.

- Cliente: carta visual con categorías, búsqueda, disponibilidad, resumen del carrito y seguimiento del pedido por pasos.
- Cocina: columnas o grupos por estado en escritorio; lista de comandas en móvil; número de pedido, tiempo y notas destacados.
- Administración: navegación lateral en escritorio, métricas diarias, carta/stock, caja y reportes con tablas legibles.
- Móvil: navegación compacta, controles táctiles y formularios desplazables sin desbordamientos.
- Componentes compartidos para botones, campos, tarjetas, estados, modales y avisos; variantes de carga, vacío y error.
- Estados acompañados de texto e iconos, foco visible y navegación por teclado. Respetar ampliación de texto y movimiento reducido.

Las capturas existentes son evidencia del diseño Android, no una validación de la futura interfaz web.

## 8. Entorno y validación realizada

- D:\desayuno_migracion existe y no contiene archivos al momento de esta revisión.
- Node v24.22.1 y npm 11.19.1 disponibles.
- Go no se encontró en PATH ni en C:\Program Files\Go\bin\go.exe. No se descarta una instalación en otra ubicación.
- Existe comando Java; no se compiló Android en esta fase.
- No existe local.properties en el checkout revisado. No se comprobó configuración secreta de variables de entorno.
- No está instalada la dependencia PGlite en .tools/sql-test/node_modules, requerida por las pruebas SQL.
- Ejecutado: node --test supabase/tests/usuarios.test.mjs. Resultado: 4 pruebas aprobadas, 0 fallidas. Usan servidor simulado; no crean cuentas reales.
- No se ejecutaron las pruebas JVM, instrumentadas ni SQL; los resultados históricos documentados no se presentan como resultados nuevos.
- No se modificó código Android ni se realizaron escrituras en el Supabase remoto.

## 9. Secuencia de migración y aceptación

1. Preparar frontend Next.js y API Go, configuración de ejemplo, ejecución local y contrato de errores. Resolver disponibilidad del compilador Go.
2. Implementar login, registro, perfil, renovación y permisos. Comprobar aislamiento entre dos clientes con el mismo nombre y rechazo de acciones administrativas desde CLIENT/COOK.
3. Migrar carta, carrito y checkout idempotente. Probar agotados, cambio de precio y reintento después de una respuesta perdida; competir por la última unidad sin sobreventa.
4. Migrar cola de cocina, estados y cancelaciones con restitución de stock una sola vez. Recuperar estado después de una desconexión.
5. Migrar edición de productos, imágenes, stock y avisos; probar conflictos de updated_at y restricciones por rol.
6. Migrar cobros, entrega atómica, reportes y caja. Probar que un fallo de entrega revierte el cobro, que el reintento no lo duplica y que el cierre no compite incorrectamente con nuevos pagos.
7. Completar centro de avisos y adaptación visual; verificar móvil/escritorio, teclado, textos largos, estados vacíos y errores. Definir push/PWA como ampliación si se requiere recepción con navegador cerrado.
8. Validar en ensayo con la base real respaldada, conciliar pedidos/importes/stock e imágenes y planificar el cambio de uso. Si Android y web conviven, mantener compatibilidad del contrato de base durante la transición.

El siguiente paso técnico es construir la base Next.js + Go y el primer flujo completo de acceso y carta. La conexión a datos reales requiere comprobar configuración y estado efectivo del proyecto Supabase antes de alterar su esquema.

