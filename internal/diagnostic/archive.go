package diagnostic

import (
	gitrepository "github.com/XR-Lee/Gemcp/internal/repository"
	"github.com/XR-Lee/Gemcp/internal/sourcearchive"
)

type archiveInspection = sourcearchive.Inspection

func inspectArchive(archive gitrepository.Archive, maximum int64) (archiveInspection, error) {
	return sourcearchive.Inspect(archive, maximum)
}
