//  Copyright (C) 2020 Maker Ecosystem Growth Holdings, INC.
//
//  This program is free software: you can redistribute it and/or modify
//  it under the terms of the GNU Affero General Public License as
//  published by the Free Software Foundation, either version 3 of the
//  License, or (at your option) any later version.
//
//  This program is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU Affero General Public License for more details.
//
//  You should have received a copy of the GNU Affero General Public License
//  along with this program.  If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/makerdao/gofer/pkg/gofer"
	"github.com/makerdao/gofer/pkg/gofer/config"
	configJSON "github.com/makerdao/gofer/pkg/gofer/config/json"
	"github.com/makerdao/gofer/pkg/log"
	logLogrus "github.com/makerdao/gofer/pkg/log/logrus"
)

func main() {
	var opts options
	rootCmd := NewRootCommand(&opts)

	rootCmd.AddCommand(
		NewOriginsCmd(&opts),
		NewPairsCmd(&opts),
		NewPricesCmd(&opts),
		NewServerCmd(&opts),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newLogger(level string) (log.Logger, error) {
	ll, err := logrus.ParseLevel(level)
	if err != nil {
		return nil, err
	}

	lr := logrus.New()
	lr.SetLevel(ll)

	return logLogrus.New(lr), nil
}

func newGofer(opts *options, path string, l log.Logger) (*gofer.Gofer, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	err = configJSON.ParseJSONFile(&opts.Config, absPath)
	if err != nil {
		return nil, err
	}

	i, err := opts.Config.Configure(config.Dependencies{Logger: l})
	if err != nil {
		return nil, err
	}

	return i.Gofer, nil
}

// asyncCopy asynchronously copies from src to dst using the io.Copy.
// The returned function will block the current goroutine until
// the io.Copy finished.
func asyncCopy(dst io.Writer, src io.Reader) func() {
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		_, err := io.Copy(dst, src)
		wg.Done()

		if err == io.EOF {
			return
		}
		if err != nil {
			panic(err.Error())
		}
	}()

	return func() {
		wg.Wait()
	}
}
