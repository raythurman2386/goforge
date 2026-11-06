package docker

import cerrdefs "github.com/containerd/errdefs"

// IsNotFound reports whether Docker no longer has the requested object.
func IsNotFound(err error) bool {
	return cerrdefs.IsNotFound(err)
}
