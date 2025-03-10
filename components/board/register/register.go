// Package register registers all relevant Boards and also API specific functions
package register

import (
	// for boards.
	_ "go.viam.com/rdk/components/board/beaglebone"
	_ "go.viam.com/rdk/components/board/fake"
	_ "go.viam.com/rdk/components/board/hat/pca9685"
	_ "go.viam.com/rdk/components/board/jetson"
	_ "go.viam.com/rdk/components/board/nanopi"
	_ "go.viam.com/rdk/components/board/numato"
	_ "go.viam.com/rdk/components/board/pi"
	_ "go.viam.com/rdk/components/board/ti"
)
