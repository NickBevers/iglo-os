package setup

import (
	"context"

	"iglo/internal/immich"
)

// immichKey creates the first admin of a new Immich and returns an API key
// for Background work, which may only read and pause the queues. A Photos
// install from the Apps page does the same through internal/immich.
func (s *Service) immichKey(ctx context.Context, server string, admin Admin, allowRestrictedFaces bool) (string, error) {
	client := immich.Client{HTTP: s.http(), Server: server}
	token, err := client.CreateAdmin(ctx, immich.Admin{Email: admin.Email, Name: admin.Username, Password: admin.Password, FaceModels: allowRestrictedFaces})
	if err != nil {
		return "", err
	}
	return client.QueueKey(ctx, token)
}
