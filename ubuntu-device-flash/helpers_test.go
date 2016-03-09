// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	. "gopkg.in/check.v1"
)

type HTestSuite struct{}

var _ = Suite(&HTestSuite{})

func makeTestFiles(c *C, srcDir, destDir string) {
	// a new file
	err := ioutil.WriteFile(filepath.Join(srcDir, "new"), []byte(nil), 0644)
	c.Assert(err, IsNil)

	// a existing file that needs update
	err = ioutil.WriteFile(filepath.Join(destDir, "existing-update"), []byte("old-content"), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(srcDir, "existing-update"), []byte("some-new-content"), 0644)
	c.Assert(err, IsNil)

	// existing file that needs no update
	err = ioutil.WriteFile(filepath.Join(srcDir, "existing-unchanged"), []byte(nil), 0644)
	c.Assert(err, IsNil)
	err = exec.Command("cp", "-a", filepath.Join(srcDir, "existing-unchanged"), filepath.Join(destDir, "existing-unchanged")).Run()
	c.Assert(err, IsNil)

	// a file that needs removal
	err = ioutil.WriteFile(filepath.Join(destDir, "to-be-deleted"), []byte(nil), 0644)
	c.Assert(err, IsNil)
}

func compareDirs(c *C, srcDir, destDir string) {
	d1, err := exec.Command("ls", "-al", srcDir).CombinedOutput()
	c.Assert(err, IsNil)
	d2, err := exec.Command("ls", "-al", destDir).CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(string(d1), Equals, string(d2))
	// ensure content got updated
	c1, err := exec.Command("sh", "-c", fmt.Sprintf("find %s -type f |xargs cat", srcDir)).CombinedOutput()
	c.Assert(err, IsNil)
	c2, err := exec.Command("sh", "-c", fmt.Sprintf("find %s -type f |xargs cat", destDir)).CombinedOutput()
	c.Assert(err, IsNil)
	c.Assert(string(c1), Equals, string(c2))
}

func (ts *HTestSuite) TestSyncDirs(c *C) {

	for _, l := range [][2]string{
		[2]string{"src-short", "dst-loooooooooooong"},
		[2]string{"src-loooooooooooong", "dst-short"},
		[2]string{"src-eq", "dst-eq"},
	} {

		// ensure we have src, dest dirs with different length
		srcDir := filepath.Join(c.MkDir(), l[0])
		err := os.MkdirAll(srcDir, 0755)
		c.Assert(err, IsNil)
		destDir := filepath.Join(c.MkDir(), l[1])
		err = os.MkdirAll(destDir, 0755)
		c.Assert(err, IsNil)

		// add a src subdir
		subdir := filepath.Join(srcDir, "subdir")
		err = os.Mkdir(subdir, 0755)
		c.Assert(err, IsNil)
		makeTestFiles(c, subdir, destDir)

		// add a dst subdir that needs to get deleted
		subdir2 := filepath.Join(destDir, "to-be-deleted-subdir")
		err = os.Mkdir(subdir2, 0755)
		subdir3 := filepath.Join(subdir2, "to-be-deleted-sub-subdir")
		err = os.Mkdir(subdir3, 0755)

		// and a toplevel
		makeTestFiles(c, srcDir, destDir)

		// do it
		err = RSyncWithDelete(srcDir, destDir)
		c.Assert(err, IsNil)

		// ensure meta-data is identical
		compareDirs(c, srcDir, destDir)
		compareDirs(c, filepath.Join(srcDir, "subdir"), filepath.Join(destDir, "subdir"))
	}
}

func (ts *HTestSuite) TestSyncDirFails(c *C) {
	srcDir := c.MkDir()
	err := os.MkdirAll(srcDir, 0755)
	c.Assert(err, IsNil)

	destDir := c.MkDir()
	err = os.MkdirAll(destDir, 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(destDir, "meep"), []byte(nil), 0644)
	c.Assert(err, IsNil)

	// ensure remove fails
	err = os.Chmod(destDir, 0100)
	c.Assert(err, IsNil)
	// make tempdir cleanup work again
	defer os.Chmod(destDir, 0755)

	// do it
	err = RSyncWithDelete(srcDir, destDir)
	c.Check(err, NotNil)
	c.Check(err, ErrorMatches, ".*permission denied.*")
}
