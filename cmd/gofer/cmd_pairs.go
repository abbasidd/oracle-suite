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
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/makerdao/gofer/internal/gofer/cli"
	"github.com/makerdao/gofer/internal/gofer/marshal"
)

func NewPairsCmd(o *options) *cobra.Command {
	return &cobra.Command{
		Use:     "pairs",
		Aliases: []string{"pair"},
		Args:    cobra.NoArgs,
		Short:   "List all supported pairs",
		Long:    `List all supported asset pairs.`,
		RunE: func(_ *cobra.Command, args []string) error {
			m, err := marshal.NewMarshal(o.OutputFormat.format)
			if err != nil {
				return err
			}

			wait := asyncCopy(os.Stdout, m)
			defer func() {
				_ = m.Close()
				wait()
			}()

			absPath, err := filepath.Abs(o.ConfigFilePath)
			if err != nil {
				return err
			}

			l, err := newLogger(o.LogVerbosity)
			if err != nil {
				return err
			}

			g, err := newGofer(o, absPath, l)
			if err != nil {
				return err
			}

			err = cli.Pairs(g, m)
			if err != nil {
				return err
			}

			return nil
		},
	}
}
