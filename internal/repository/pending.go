package repository

const ObservationWritesPendingNote = "Graph and experiment-catalog observation writes remain possible while status is pending_key. Prepare of a git-backed Experiment still requires verify."

func ObservationWritesAllowed(status string) bool {
	return status != "disabled"
}

func PendingNote(status string) string {
	if status == "pending_key" || status == "error" {
		return ObservationWritesPendingNote
	}
	return ""
}
