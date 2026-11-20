package cloudsync

// The connection idle timeout is adapted from rclone fs/fshttp/dialer.go,
// revision e982ff36668f5130d94b4f70409babb3d964df42 (v1.76.0), under MIT.
// See third_party/rclone/LICENSE. Bandwidth accounting is not included.
import (
	"net"
	"time"
)

// idleTimeoutConn allows an active transfer to continue while bounding time
// with no traffic, including a response body that stalls after its headers.
type idleTimeoutConn struct {
	net.Conn
	timeout time.Duration
}

func (c *idleTimeoutConn) nudgeDeadline() error {
	if c.timeout > 0 {
		return c.SetDeadline(time.Now().Add(c.timeout))
	}
	return nil
}

func (c *idleTimeoutConn) Read(b []byte) (n int, err error) {
	n, err = c.Conn.Read(b)
	if err == nil && n > 0 {
		err = c.nudgeDeadline()
	}
	return n, err
}

func (c *idleTimeoutConn) Write(b []byte) (n int, err error) {
	n, err = c.Conn.Write(b)
	if err == nil && n > 0 {
		err = c.nudgeDeadline()
	}
	return n, err
}
