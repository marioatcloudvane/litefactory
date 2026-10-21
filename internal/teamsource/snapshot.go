package teamsource

// MaxSourceBytes bounds one source or resource read.
const MaxSourceBytes int64 = 1 << 20

// MaxSnapshotBytes bounds all source and resource bytes in one capture.
const MaxSnapshotBytes = 16 << 20
