# 09: qué va en la app y qué en la web

Decisión de producto (dueño, 2026-10-30). La **web** es el control general: se entra desde una computadora, de vez
en cuando, para configurar y revisar. La **app** es la herramienta de mano del personal (admin y recepcionista): pocas
cosas, rápidas, fáciles de entender y de llenar. Regla para decidir dónde va algo nuevo:

> Si se hace de pie, en segundos, varias veces al día → app. Si se revisa, se configura o se analiza → web.
> Nada se duplica "por si acaso": una pantalla vive en un solo lado, salvo que haya una razón clara.

## Matriz

| Función | App | Web | Nota |
|---|---|---|---|
| Buscar miembro (nombre o documento) | Sí | Sí | Acción más frecuente. |
| Ficha del miembro: estado, planes, vencimientos | Sí | Sí | |
| Crear miembro + primera suscripción | Sí | Sí | Formulario corto. |
| Renovar / añadir plan / modificar suscripción | Sí | Sí | Ver regla de solapamiento abajo. |
| Por vencer (llamar, WhatsApp, renovar) | Sí | Sí | Atajo en la app. |
| Venta rápida de productos | Sí | Sí (tablet/PC) | Módulo `sales`. |
| Venta pendiente de un miembro y cobro posterior | Sí | Sí (ficha del miembro) | Módulo `sales`. |
| Medida rápida (peso) y rutina en la ficha | Sí | Sí (historial y gráfico) | Módulo `body_tracking`. |
| Control de acceso del día | No | Sí | Lo hace el quiosco; recepción puede usar la web. |
| Quiosco (tablet en la puerta) | No | Sí | Modo quiosco en pulso-web. |
| Resumen / dashboard / KPIs | No | Sí | Se revisa semanalmente en computadora. |
| Pagos: lista, filtros, exportar CSV | No | Sí | |
| Reportes (incluye ventas de productos) | No | Sí | |
| Planes y tarifas | No | Sí | Configuración. |
| Catálogo de productos | No | Sí | Configuración. |
| Equipo (usuarios, roles, sedes) | No | Sí | |
| Identidad del gym, módulos, grupos, sedes | No | Consola | Solo el superadmin. |
| Tablets registradas | No | Sí | Fase 2 del quiosco. |

## Regla de solapamiento de membresías

Un miembro no necesita una membresía nueva si ya tiene una vigente del **mismo plan**:
- Comprar el mismo plan mientras está vigente se trata como **renovación**: empieza el día en que termina la actual
  (queda "programada") y no se puede solapar con otra del mismo plan.
- Planes distintos sí pueden convivir al mismo tiempo (Baile + Todo el gym).
- La interfaz propone "Renovar" en vez de "Nueva membresía" cuando ya hay una vigente del mismo plan.

## Wallet (más adelante)

Ver `10-wallet-passes.md`: pase de Apple Wallet / Google Wallet con QR que el quiosco escanea.
