package account

import "errors"

// ErrNotFound is returned by repositories when a row cannot be located.
var ErrNotFound = errors.New("account: not found")

// ErrAvatarTooLarge is returned when an uploaded avatar exceeds the size cap.
var ErrAvatarTooLarge = errors.New("account: avatar too large")

// ErrAvatarUnsupported is returned when the content-type is not in the
// allow-list (image/png, image/jpeg, image/webp).
var ErrAvatarUnsupported = errors.New("account: avatar content-type unsupported")
