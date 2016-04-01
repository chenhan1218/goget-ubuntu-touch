//
// diskimage - handles ubuntu disk images
//
// Copyright (c) 2015 Canonical Ltd.
//
// Written by Sergio Schvezov <sergio.schvezov@canonical.com>
//
package diskimage

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
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

type CommonTestSuite struct {
	tmpdir      string
	gadget      GadgetDescription
	packageInst string
}

var _ = Suite(&CommonTestSuite{})

func (s *CommonTestSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	s.gadget = GadgetDescription{Name: "packagename", Version: "42"}
	s.packageInst = s.gadget.Name
}

func (s *CommonTestSuite) TestOemInstallPath(c *C) {
	err := os.MkdirAll(filepath.Join(s.tmpdir, "gadget", s.packageInst, "current"), 0755)
	c.Assert(err, IsNil)

	s.gadget.SetRoot(s.tmpdir)
	installPath, err := s.gadget.InstallPath()

	c.Assert(err, IsNil)
	c.Assert(installPath, Equals, filepath.Join(s.tmpdir, "gadget/packagename/current"))
}

func (s *CommonTestSuite) TestOemInstallPathNoOem(c *C) {
	err := os.MkdirAll(filepath.Join(s.tmpdir, "gadget", s.packageInst), 0755)
	c.Assert(err, IsNil)

	s.gadget.SetRoot(s.tmpdir)
	installPath, err := s.gadget.InstallPath()

	c.Assert(err, NotNil)
	c.Assert(installPath, Equals, "")
}
