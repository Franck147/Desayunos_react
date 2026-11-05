# Personajes ficticios de prueba

Estas cuentas son ficticias y se crearon expresamente para probar el login local con SQLite. No representan personas reales.

| Personaje | Acceso | Usuario | Correo | Contraseña |
| --- | --- | --- | --- | --- |
| Valentina Rojas | Cliente | cliente | cliente@demo.local | Valentina2026! |
| Diego Mendoza | Cliente | diego | diego@demo.local | Diego2026! |
| Marco Ramos | Usuario (cocina) | cocina | cocina@demo.local | Marco2026! |
| Adriana Torres | Admin | admin | admin@demo.local | Adriana2026! |

Valentina es una clienta habitual que pide pan con palta; Diego prueba los combos. Marco prepara y entrega las comandas. Adriana administra la carta, los cobros y la caja.

Selecciona Cliente, Usuario o Admin en el login y escribe el usuario corto o el correo completo. La contraseña distingue mayúsculas. El servidor verifica el rol; elegir Admin no concede permisos a una cuenta de cliente o cocina.

Las contraseñas se guardan como hashes PBKDF2 con sal individual en SQLite. Esta tabla contiene credenciales conocidas de prueba: utiliza cuentas con contraseñas propias al pasar a uso real.

Se crearon en .data/desayunos.db y .data/demo.db. Las bases mantienen sus datos separados.

Para preparar otra instalación:

```powershell
.\crear-personajes.ps1
.\crear-personajes.ps1 -Demo
```

La operación es explícita: no se crean cuentas privilegiadas automáticamente al arrancar. Repetir el script conserva cuentas, roles y contraseñas ya configuradas. Solo habilita los personajes demo originales que todavía tenían la contraseña desactivada. Si un correo pertenece a otra identidad o rol, revierte el alta completa sin modificarla.

Inicia con .\iniciar.ps1 para usar la base normal o .\iniciar.ps1 -Demo para ver productos y pedidos de ejemplo. El registro público sigue creando únicamente clientes.
