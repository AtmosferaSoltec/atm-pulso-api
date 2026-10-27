# documents module

Looks up Peruvian documents so the web can fill names and legal names: a **DNI** gives the
person's names, a **RUC** gives the legal name and address.

The data comes from `atm-identity-api` (our internal service, which caches the paid upstream).
Pulso keeps **no copy**: nothing is stored here. In development, without `IDENTITY_API_KEY`, an
`identity.Fake` answers stable invented data (`00000000` and `00000000000` are "not found").

Carné de extranjería is not resolved by identity-api: the web asks for it manually.

## Ports
`app.Directory`, implemented by `adapters/directory` on top of `internal/integrations/identity`.

## Errors
| code | status | when |
|---|---|---|
| `VALIDATION_FAILED` | 400 | not 8 digits (DNI) or not 11 digits (RUC) |
| `DOCUMENT_NOT_FOUND` | 404 | the document does not exist |
| `LOOKUP_UNAVAILABLE` | 502 | identity-api or its upstream failed; the web falls back to manual entry |

## Privacy
Document numbers are personal data: they are never logged, and neither are the answers.
