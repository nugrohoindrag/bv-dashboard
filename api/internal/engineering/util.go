package engineering

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/buildingvision/api/internal/platform/authctx"
)

func principalOrg(r *http.Request) uuid.UUID { return authctx.Must(r.Context()).OrganizationID }
