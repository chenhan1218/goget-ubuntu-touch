//
// ubuntu-emu - Tool to download and run Ubuntu Touch emulator instances
//
// Copyright (c) 2013 Canonical Ltd.
//
// Written by Sergio Schvezov <sergio.schvezov@canonical.com>
//
package sysutils

// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License version 3, as published
// by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranties of
// MERCHANTABILITY, SATISFACTORY QUALITY, or FITNESS FOR A PARTICULAR
// PURPOSE.  See the GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along
// with this program.  If not, see <http://www.gnu.org/licenses/>.

import (
	"bufio"
	"encoding/json"
	"launchpad.net/goget-ubuntu-touch/ubuntuimage"
	"os"
	"path/filepath"
)

func WriteStamp(dataDir string, image ubuntuimage.Image) (err error) {
	path := filepath.Join(dataDir, ".stamp")
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		file.Close()
		if err != nil {
			os.Remove(path)
		}
	}()
	w := bufio.NewWriter(file)
	defer w.Flush()
	jsonWriter := json.NewEncoder(w)
	if err := jsonWriter.Encode(image); err != nil {
		return err
	}

	return nil
}

func ReadStamp(dataDir string) (image ubuntuimage.Image, err error) {
	path := filepath.Join(dataDir, ".stamp")
	file, err := os.Open(path)
	if err != nil {
		return image, err
	}
	jsonReader := json.NewDecoder(file)
	if err := jsonReader.Decode(&image); err != nil {
		return image, err
	}

	return image, nil
}