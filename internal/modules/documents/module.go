// Package documents is the module that looks up Peruvian documents (DNI and RUC) so the
// web can fill names and legal names for the user. See README.md.
package documents

import (
	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-pulso-api/internal/integrations/identity"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents/adapters/directory"
	documentshttp "github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents/adapters/http"
	"github.com/AtmosferaSoltec/atm-pulso-api/internal/modules/documents/app"
)

// Module is the assembled documents module.
type Module struct {
	handler *documentshttp.Handler
}

// New wires the module on top of an identity-api client (or the development fake).
func New(client identity.Client) *Module {
	service := app.NewService(directory.New(client))
	return &Module{handler: documentshttp.NewHandler(service)}
}

// Routes mounts the module's endpoints.
func (m *Module) Routes(protected gin.IRoutes) { m.handler.Routes(protected) }
