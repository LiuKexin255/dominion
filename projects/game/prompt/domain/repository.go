package domain

import "context"

// TeamProfileRepository defines storage operations for TeamProfile entities.
type TeamProfileRepository interface {
	// CreateTeamProfile stores a new TeamProfile.
	CreateTeamProfile(ctx context.Context, profile *TeamProfile) error
	// GetTeamProfile retrieves a TeamProfile by template and profile name.
	// It returns ErrNotFound if no profile with the given name exists under
	// the template.
	GetTeamProfile(ctx context.Context, template, profileName string) (*TeamProfile, error)
	// UpdateTeamProfile replaces the stored TeamProfile identified by
	// profile.TeamProfileName. It returns ErrNotFound if no profile with the
	// given name exists.
	UpdateTeamProfile(ctx context.Context, profile *TeamProfile) (*TeamProfile, error)
	// ListTeamProfiles retrieves a page of TeamProfiles under a template.
	// pageSize controls the maximum number of results; pageToken is the cursor
	// for the next page. Pass empty string for the first page.
	ListTeamProfiles(ctx context.Context, template string, pageSize int, pageToken string) ([]*TeamProfile, string, error)
	// DeleteTeamProfile removes a TeamProfile by template and profile name.
	// It returns ErrNotFound if no profile with the given name exists under
	// the template.
	DeleteTeamProfile(ctx context.Context, template, profileName string) error
}
