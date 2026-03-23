package app

import (
	"context"
	"io"
	"os"

	"github.com/p-society/raag/internal/domain"
)

type ResolveFunc func(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error)

func NewResolveFunc(libraryRepo LibraryRepository) ResolveFunc {
	return func(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
		track, err := libraryRepo.FindByID(ctx, trackID)
		if err != nil {
			return nil, err
		}

		file, err := os.Open(track.Path)
		if err != nil {
			return nil, err
		}
		return file, nil
	}
}
