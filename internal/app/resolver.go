package app

import (
	"context"
	"io"
	"os"

	"github.com/p-society/raag/internal/domain"
)

type Resolver struct {
	libraryRepo LibraryRepository
}

func NewResolver(libraryRepo LibraryRepository) *Resolver {
	return &Resolver{
		libraryRepo: libraryRepo,
	}
}

func (s *Resolver) Resolve(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
	track, err := s.libraryRepo.FindByID(ctx, trackID)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(track.Path)
	if err != nil {
		return nil, err
	}
	return file, nil
}
