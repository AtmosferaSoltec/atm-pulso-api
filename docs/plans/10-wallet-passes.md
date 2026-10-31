# 10: pases en Apple Wallet y Google Wallet (diseño, sin implementar)

Estado: propuesta para decisión del dueño. Hoy no hay app de miembro ni cuentas de miembro; el acceso se hace
tecleando el documento en el quiosco (plan 04/08). Meta: el miembro muestra un QR desde su Wallet y entra.

Nota de verificación: las páginas de developer.apple.com cargan con JavaScript y mi lector solo recuperó títulos;
lo marcado **[verificado]** salió de páginas oficiales de Google/MDN que sí se leyeron. Lo marcado **[no verificado]**
viene de conocimiento general o de fuentes no oficiales y debe confirmarse en la fuente indicada antes de comprometer
fechas o costos.

## 1. Qué es viable y qué no

| Opción | Viable | Condición |
|---|---|---|
| Pase con QR/código de barras (Apple y Google) | **Sí, recomendado** | Cuentas de desarrollador/emisor; el quiosco lo lee con la cámara. |
| Pase con NFC (Apple VAS / Google Smart Tap) | **No para el primer lanzamiento** | Aprobación especial + lector NFC compatible en la puerta. |

- **QR (genérico).** Apple: `pass.json` admite `barcodes` con formato `PKBarcodeFormatQR` y `messageEncoding`
  [no verificado, [docs](https://developer.apple.com/documentation/walletpasses/pass)]. Google: el objeto genérico tiene
  `barcode` y `rotatingBarcode`, `state`, `hexBackgroundColor`, y se actualiza con `patch`
  **[verificado]** ([GenericObject](https://developers.google.com/wallet/generic/rest/v1/genericobject)).
- **NFC en Apple.** La clave `nfc` del pase solo funciona con un permiso aparte de Apple ligado al Pass Type ID, firmado
  con certificado "Apple VAS"; sin él, el NFC no hace nada. **[no verificado en doc oficial]**: lo dicen un
  ingeniero de Apple en el [foro](https://developer.apple.com/forums/thread/769877) y guías de terceros; la solicitud
  se hace por el [formulario de contacto PassKit](https://developer.apple.com/contact/passkit/nfc-certificate). No encontré criterios ni plazos oficiales.
- **NFC en Google (Smart Tap).** El terminal se identifica con un Collector ID de 8 dígitos y el teléfono lo autentica con una
  clave pública; el proveedor del terminal carga la clave privada **[verificado]**
  ([Collector IDs](https://developers.google.com/wallet/smart-tap/introduction/collection-identifiers)). Las
  aprobaciones de terminales pueden ser revocadas por Google (carta de aprobación de un fabricante; no verificado en doc de Google).
  Requiere lectores certificados Smart Tap, no una tablet.
- **"Doble clic en el botón lateral como tarjeta de crédito".** Eso es NFC (en iPhone, doble clic abre Wallet y el
  teléfono se acerca a un lector). No se puede lograr con una tablet corriente como quiosco: haría falta comprar lectores
  NFC certificados y obtener las aprobaciones anteriores. Lo realista: el miembro abre el pase en Wallet (en iPhone el
  doble clic abre Wallet, y el pase puede aparecer automáticamente) y **muestra el QR a la cámara de la tablet**. Es
  casi igual de rápido y no depende de hardware. NFC queda como fase futura opcional, evaluada con un gimnasio que lo pida.

Fuentes de lectura web: la API [`BarcodeDetector`](https://developer.mozilla.org/en-US/docs/Web/API/Barcode_Detection_API)
es **experimental y de disponibilidad limitada**, solo en HTTPS, lee `qr_code` **[verificado]**. Qué navegadores la traen
(Chrome Android sí; Safari de iPad no confirmado) hay que mirar en la tabla de compatibilidad de MDN. Por eso el quiosco
debe usar una **librería JS de respaldo** (p. ej. `zxing-wasm` o `jsQR`; elección de dependencia pendiente) con
`getUserMedia`, que exige HTTPS y permiso de cámara.

## 2. Cuentas, costos y entregables del dueño

**Apple Wallet** (los costos son de conocimiento general, confirmar):
- Apple Developer Program: unos US$99 al año **[no verificado]**. Una sola cuenta de Atmósfera puede servir a todos los gimnasios
  (un Pass Type ID, p. ej. `pass.pe.atmosfera.pulso`; el color y logo cambian por pase).
- Pass Type ID + certificado de firma (`.p12`) + certificado intermedio WWDR de Apple. El mismo certificado firma
  los pases y las notificaciones push (APNs) de actualización **[no verificado]**. El certificado vence (renovación anual aprox.).
- El Pass Type ID no se puede cambiar una vez creado: definir el nombre con cuidado.

**Google Wallet** ([prerrequisitos](https://developers.google.com/wallet/generic/web/prerequisites) **[verificado]** salvo costo):
- Cuenta de emisor en Google Pay & Wallet Console (la página oficial no habla de costo; no encontré tarifa, **[no verificado]**).
- Crear al menos una clase de pase (plantilla; usaremos el tipo "Generic") y solicitar acceso de publicación; mientras no
  lo aprueben solo funcionan cuentas de prueba.
- Cuenta de servicio de Google Cloud con su clave JSON; los pases se firman como JWT con esa clave (enlace "Save to Google Wallet").

**Lista exacta que el dueño debe entregar:**
1. Acceso (o creación guiada) de la cuenta Apple Developer a nombre de Atmósfera; Team ID.
2. Pass Type ID creado, certificado `.p12` con su contraseña y confirmar el WWDR vigente.
3. ID del emisor de Google Wallet (Issuer ID) y aprobación del acceso de publicación.
4. Clave JSON de la cuenta de servicio de Google.
5. Un dominio HTTPS público para la API (el web service de Apple lo exige).
6. Por gimnasio: logo (PNG cuadrado y ancho) y color; ya existen en la consola.
7. Decisión de costos: Apple anual + posible costo de Google (a confirmar) + el tiempo de aprobación.

Los secretos van como variables de entorno del servidor (nunca en el repositorio), como el resto de la configuración.

## 3. Diseño recomendado

**Contenido del QR.** Un token opaco, nunca el DNI: `pulso1.<member_public_id>.<version>.<firma>`, donde la firma es
HMAC-SHA256 (clave del servidor, con versión de clave para rotarla) sobre gimnasio+miembro+versión. Sin datos personales y no
adivinable. Se guarda en `members` (o tabla `member_passes`) un `pass_version`/`revoked_at`: revocar = subir la versión,
con lo que el QR anterior deja de valer y el pase se actualiza solo. Rotación automática del QR (Google tiene
`rotatingBarcode`; Apple no lo tiene que yo haya verificado) es opcional: el riesgo principal es una captura de pantalla
compartida, que se mitiga con revocación y el aviso de "ya ingresó hoy" que el control de acceso ya da.

**Validación.** Un endpoint del quiosco, `POST /access/scan { token, branch_id }`, que decodifica el token, resuelve el
miembro **del gimnasio del principal** (token de otro gimnasio = 404, regla de tenancy) y entra por el mismo caso de uso del
control de acceso por documento (membresía vigente, `branch_sharing`, sede y registro de ingreso). La respuesta es la misma que hoy.
Solo cambia la forma de identificar al miembro. Verificar firma en tiempo constante.

**Emisión.** `POST /members/{id}/wallet-pass` (admin o recepción con acceso a la sede del miembro) crea un enlace corto de un solo uso
y de vida limitada; recepción lo envía por WhatsApp (ya se usa para vencimientos). La página pública del enlace detecta
el sistema y ofrece "Agregar a Apple Wallet" (descarga `.pkpass`, generado y firmado por la API) o "Guardar en Google Wallet"
(URL con JWT). Sin cuentas de miembro: el enlace es la autorización, por eso es de un solo uso y expira.

**Actualización.** Apple: implementar el web service de PassKit (registrar/borrar dispositivos, listar pases cambiados,
descargar pase, log) con `authenticationToken` por pase y notificación push APNs vacía al cambiar la membresía; los endpoints
exactos hay que confirmarlos en la [doc de Apple](https://developer.apple.com/documentation/walletpasses/adding-a-web-service-to-update-passes) [no verificado]. Google: `PATCH` del objeto
(`state` activo/inactivo, textos). Disparador: pago, renovación, vencimiento y revocación (cola/tarea en segundo plano).

**Identidad y privacidad.** Color y logo del gimnasio en cada pase. Mostrar solo: nombre del gimnasio, nombre del miembro,
estado y fecha de vencimiento del plan. No mostrar el documento ni el teléfono. Texto del QR sin datos legibles.
Una pantalla bloqueada puede mostrar el pase: la fecha es poco sensible, pero el dueño debe aceptarlo.

**Quiosco y feature flags.** El modo quiosco abre la cámara trasera/frontal con `getUserMedia`, intenta `BarcodeDetector` y cae a la
librería JS; un escaneo exitoso llama a `/access/scan` y muestra el mismo resultado de hoy. El teclado de DNI se mantiene como
alternativa. Módulo de pago por gimnasio `wallet` (como `products` y `body_tracking`, plan 08): oculta la emisión de pases y
rechaza `/access/scan` y `/wallet-pass` con el error de módulo desactivado cuando el gimnasio no lo tiene. El escaneo de QR en sí
podría ir en el módulo base `kiosk`; decisión del dueño.

## 4. Fases, esfuerzo y riesgos

| Fase | Contenido | Esfuerzo | Necesita cuentas |
|---|---|---|---|
| 0 | Token firmado + revocación + `/access/scan` + escaneo con cámara en el quiosco (también sirve con un QR impreso o una imagen enviada por WhatsApp) | M | No |
| 1 | Módulo `wallet`, enlace de emisión de un solo uso, página pública, mensaje de WhatsApp | S | No |
| 2 | Google Wallet: clase, objeto genérico, JWT, PATCH al cambiar membresía | M | Sí (Google) |
| 3 | Apple Wallet: `.pkpass` firmado + web service + APNs | L | Sí (Apple) |
| 4 (opcional) | NFC (Smart Tap / VAS) con lectores certificados | L, incierto | Aprobaciones y hardware |

**Riesgos.** (a) Tiempos de aprobación de Google y de Apple desconocidos; (b) `BarcodeDetector` incompleto en tablets, mitigado con
la librería de respaldo; la cámara en interiores con poca luz y el brillo de pantallas del teléfono; (c) QR compartido por captura de pantalla;
(d) el certificado de Apple vence y rompe la emisión y las actualizaciones; (e) ligar a un número de teléfono fijo el envío por WhatsApp sin
consentimiento del miembro (tratamiento de datos personales, Ley 29733 en Perú, sin verificar); (f) un solo Pass Type ID compartido entre
gimnasios: el marcado "Atmósfera" puede verse en el reverso del pase.

**Se puede dejar listo ya, sin cuentas:** la fase 0 completa y la fase 1. Esto entrega valor inmediato (QR por WhatsApp) y deja el
Wallet como un envoltorio que solo agrega el mismo token.
