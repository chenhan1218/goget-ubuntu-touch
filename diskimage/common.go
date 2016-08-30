//
// diskimage - handles ubuntu disk images
//
// Copyright (c) 2013 Canonical Ltd.
//
// Written by Sergio Schvezov <sergio.schvezov@canonical.com>
//
package diskimage

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

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

var debugPrint bool

func init() {
	if debug := os.Getenv("DEBUG_DISK"); debug != "" {
		debugPrint = true
	}
}

const (
	hardwareFileName = "hardware.yaml"
	kernelFileName   = "vmlinuz"
	initrdFileName   = "initrd.img"
)

var (
	syscallSync = syscall.Sync
)

type Image interface {
	Mount() error
	Unmount() error
	Format() error
	Partition() error
	BaseMount() string
}

type SystemImage interface {
	Boot() string
	System() string
	Writable() string
}

type CoreImage interface {
	Image
	SystemImage
	SetupBoot() error
	FlashExtra() error
}

type HardwareDescription struct {
	Kernel string `yaml:"kernel"`
	Dtbs   string `yaml:"dtbs"`
	Initrd string `yaml:"initrd"`
}

type BootAssetRawFiles struct {
	Path   string `yaml:"path"`
	Offset string `yaml:"offset"`
}

type BootAssetRawPartitions struct {
	Name string `yaml:"name"`
	Size int    `yaml:"size"`
	Pos  int    `yaml:"pos"`
	Type string `yaml:"type"`
}

type BootAssetFiles struct {
	Path string `yaml:"path"`
	// Target is the deprecated target relative to $bootloader dir
	Target string `yaml:"target,omitempty"`
	// Dst is the destination relative to the actual boot partition
	Dst string `yaml:"dst,omitempty"`
}

type BootAssets struct {
	Files         []BootAssetFiles         `yaml:"files,omitempty"`
	RawFiles      []BootAssetRawFiles      `yaml:"raw-files,omitempty"`
	RawPartitions []BootAssetRawPartitions `yaml:"raw-partitions,omitempty"`
}

type GadgetDescription struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`

	Gadget struct {
		Hardware struct {
			Bootloader      string      `yaml:"bootloader"`
			PartitionLayout string      `yaml:"partition-layout"`
			Dtb             string      `yaml:"dtb,omitempty"`
			Platform        string      `yaml:"platform"`
			Architecture    string      `yaml:"architecture"`
			BootAssets      *BootAssets `yaml:"boot-assets,omitempty"`
		} `yaml:"hardware,omitempty"`

		Software struct {
			BuiltIn      []string `yaml:"built-in,omitempty"`
			Preinstalled []string `yaml:"preinstalled,omitempty"`
		} `yaml:"software,omitempty"`

		Store *struct {
			ID string `yaml:"id,omitempty"`
		}
	} `yaml:"gadget,omitempty"`

	Config struct {
		UbuntuCore struct {
			Modprobe *string `yaml:"modprobe,omitempty"`
		} `yaml:"ubuntu-core,omitempty"`
	} `yaml:"config,omitempty"`

	rootDir string
}

func (o *GadgetDescription) SetRoot(rootDir string) {
	o.rootDir = rootDir
}

// SystemParts returns the system labels depending on the partition layout.
//
// The default is to return a flat structure for any unknown layout.
func (o *GadgetDescription) SystemParts() []string {
	switch o.Gadget.Hardware.PartitionLayout {
	default:
		return []string{""}
	}
}

func (o GadgetDescription) InstallPath() (string, error) {
	return o.rootDir, nil
}

func (o GadgetDescription) Architecture() string {
	return o.Gadget.Hardware.Architecture
}

func (o *GadgetDescription) SetArchitecture(architecture string) {
	o.Gadget.Hardware.Architecture = architecture
}

func (o GadgetDescription) PartitionLayout() string {
	return o.Gadget.Hardware.PartitionLayout
}

func (o GadgetDescription) Platform() string {
	return o.Gadget.Hardware.Platform
}

func (o *GadgetDescription) SetPlatform(platform string) {
	o.Gadget.Hardware.Platform = platform
}

func sectorSize(dev string) (string, error) {
	out, err := exec.Command("blockdev", "--getss", dev).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("unable to determine block size: %s", out)
	}

	return strings.TrimSpace(string(out)), err
}

// BaseImage implements the basic primitives to manage images.
type BaseImage struct {
	baseMount  string
	bindMounts []string
	hardware   HardwareDescription
	location   string
	gadget     GadgetDescription
	parts      []partition
	partCount  int
	size       int64
	rootSize   int
	label      string
}

var bindMounts = []string{"dev", "sys", "proc", filepath.Join("sys", "firmware")}

// Mount mounts the image. This also maps the loop device.
func (img *BaseImage) Mount() error {
	if err := img.doMap(); err != nil {
		return err
	}

	baseMount, err := ioutil.TempDir(os.TempDir(), "diskimage")
	if err != nil {
		return err
	}

	//Remove Mountpoint if we fail along the way
	defer func() {
		if err != nil {
			if err := os.Remove(baseMount); err != nil {
				fmt.Println("WARNING: cannot remove", baseMount, "due to", err)
			}
		}
	}()

	// We change the mode so snappy can unpack as non root
	if err := os.Chmod(baseMount, 0755); err != nil {
		return err
	}

	for _, part := range img.parts {
		if part.fs == fsNone {
			continue
		}

		mountpoint := filepath.Join(baseMount, string(part.dir))
		if err := os.MkdirAll(mountpoint, 0755); err != nil {
			return err
		}

		dev := filepath.Join("/dev/mapper", part.loop)
		printOut("Mounting", dev, part.fs, "to", mountpoint)
		if out, errMount := exec.Command("mount", filepath.Join("/dev/mapper", part.loop), mountpoint).CombinedOutput(); errMount != nil {
			return ErrMount{dev: dev, mountpoint: mountpoint, fs: part.fs, out: out}
		}
		// this is cleanup in case one of the mounts fail
		defer func() {
			if err != nil {
				if err := exec.Command("umount", mountpoint).Run(); err != nil {
					fmt.Println("WARNING:", mountpoint, "could not be unmounted")
					return
				}

				if err := os.Remove(mountpoint); err != nil {
					fmt.Println("WARNING: could not remove ", mountpoint)
				}
			}
		}()
	}
	img.baseMount = baseMount

	mountpoints := make([]string, 0, len(bindMounts))
	if img.gadget.PartitionLayout() == "minimal" {
		mountpoints = bindMounts

		for _, d := range mountpoints {
			p := filepath.Join(baseMount, d)

			if err := os.MkdirAll(p, 0755); err != nil {
				return err
			}

			printOut("Bind mounting", d, "to", p)
			if err := bindMount(filepath.Join("/", d), p); err != nil {
				return err
			}

			img.bindMounts = append(img.bindMounts, p)
		}
	}

	return nil
}

// Unmount unmounts the image. This also unmaps the loop device.
func (img *BaseImage) Unmount() error {
	defer func() {
		if isMapped(img.parts) {
			fmt.Println("WARNING: could not unmap partitions")
		}
	}()

	if img.baseMount == "" {
		panic("No base mountpoint set")
	}

	for i := len(img.bindMounts) - 1; i >= 0; i-- {
		if err := unmount(img.bindMounts[i]); err != nil {
			return err
		}
	}
	img.bindMounts = nil

	syscallSync()

	for _, part := range img.parts {
		if part.fs == fsNone {
			continue
		}

		mountpoint := filepath.Join(img.baseMount, string(part.dir))
		if out, err := exec.Command("umount", mountpoint).CombinedOutput(); err != nil {
			lsof, _ := exec.Command("lsof", "-w", mountpoint).CombinedOutput()
			printOut(string(lsof))
			dev := filepath.Join("/dev/mapper", part.loop)
			return ErrMount{dev: dev, mountpoint: mountpoint, fs: part.fs, out: out}
		}
	}

	if err := os.RemoveAll(img.baseMount); err != nil {
		return err
	}
	img.baseMount = ""

	return img.doUnmap()
}

// doMap maps the image to loop devices
func (img *BaseImage) doMap() error {
	if isMapped(img.parts) {
		panic("cannot double map partitions")
	}

	kpartxCmd := exec.Command("kpartx", "-avs", img.location)
	stdout, err := kpartxCmd.StdoutPipe()
	if err != nil {
		return err
	}

	if err := kpartxCmd.Start(); err != nil {
		return err
	}

	loops := make([]string, 0, img.partCount)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())

		if len(fields) > 2 {
			loops = append(loops, fields[2])
		} else {
			return fmt.Errorf("issues while determining drive mappings (%q)", fields)
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if len(loops) != img.partCount {
		return ErrMapCount{expectedParts: img.partCount, foundParts: len(loops)}
	}

	mapPartitions(img.parts, loops)

	if err := kpartxCmd.Wait(); err != nil {
		return err
	}

	return nil
}

// doUnmap destroys loop devices for the partitions
func (img *BaseImage) doUnmap() error {
	if img.baseMount != "" {
		panic("cannot unmap mounted partitions")
	}

	for _, part := range img.parts {
		dmsetupCmd := []string{"dmsetup", "clear", part.loop}
		if out, err := exec.Command(dmsetupCmd[0], dmsetupCmd[1:]...).CombinedOutput(); err != nil {
			return &ErrExec{command: dmsetupCmd, output: out}
		}
	}

	kpartxCmd := []string{"kpartx", "-ds", img.location}
	if out, err := exec.Command(kpartxCmd[0], kpartxCmd[1:]...).CombinedOutput(); err != nil {
		return &ErrExec{command: kpartxCmd, output: out}
	}

	unmapPartitions(img.parts)

	return nil
}

// Format formats the image following the partition types and labels them
// accordingly.
func (img BaseImage) Format() (err error) {
	if err := img.doMap(); err != nil {
		return err
	}
	defer func() {
		if errUnmap := img.doUnmap(); errUnmap != nil {
			if err == nil {
				err = errUnmap
			} else {
				fmt.Println("WARNING: could not unmap partitions after error:", errUnmap)
			}
		}
	}()

	for _, part := range img.parts {
		dev := filepath.Join("/dev/mapper", part.loop)

		if part.fs == fsFat32 {
			cmd := []string{"mkfs.vfat", "-F", "32", "-n", string(part.label)}

			size, err := sectorSize(dev)
			if err != nil {
				return err
			}

			if size != "512" {
				cmd = append(cmd, "-s", "1")
			}

			cmd = append(cmd, "-S", size, dev)

			if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
				return &ErrExec{command: cmd, output: out}
			}
		} else {
			cmd := []string{"mkfs.ext4", "-F", "-L", string(part.label), dev}
			if out, err := exec.Command(cmd[0], cmd[1:]...).CombinedOutput(); err != nil {
				return &ErrExec{command: cmd, output: out}
			}
		}
	}

	return nil
}

// User returns the writable path
func (img BaseImage) Writable() string {
	if img.parts == nil {
		panic("img is not setup with partitions")
	}

	if img.baseMount == "" {
		panic("img not mounted")
	}

	return filepath.Join(img.baseMount, string(writableDir))
}

func (img BaseImage) pathToMount(dir directory) string {
	if img.parts == nil {
		panic("img is not setup with partitions")
	}

	if img.baseMount == "" {
		panic("img not mounted")
	}

	return filepath.Join(img.baseMount, string(dir))
}

//System returns the system path
func (img BaseImage) System() string {
	return img.pathToMount(systemADir)
}

// Boot returns the system-boot path
func (img BaseImage) Boot() string {
	return img.pathToMount(bootDir)
}

// BaseMount returns the base directory used to mount the image partitions.
func (img BaseImage) BaseMount() string {
	if img.baseMount == "" {
		panic("image needs to be mounted")
	}

	return img.baseMount
}

func (img *BaseImage) GenericBootSetup(bootPath string) error {
	gadgetRoot, err := img.gadget.InstallPath()
	if err != nil {
		return err
	}

	return setupBootAssetFiles(img.Boot(), bootPath, gadgetRoot, img.gadget.Gadget.Hardware.BootAssets.Files)
}

func (img *BaseImage) FlashExtra() error {
	gadgetRoot, err := img.gadget.InstallPath()
	if err != nil {
		return err
	}

	if bootAssets := img.gadget.Gadget.Hardware.BootAssets; bootAssets != nil {
		if bootAssets.RawPartitions != nil {
			if err := setupBootAssetRawPartitions(img.location, img.partCount, bootAssets.RawPartitions); err != nil {
				return err
			}
		}

		return setupBootAssetRawFiles(img.location, gadgetRoot, bootAssets.RawFiles)
	}

	return nil
}

func printOut(args ...interface{}) {
	if debugPrint {
		fmt.Println(args...)
	}
}

func bindMount(src, dst string) error {
	if out, err := exec.Command("mount", "--bind", src, dst).CombinedOutput(); err != nil {
		return fmt.Errorf("issues while bind mounting: %s", out)
	}

	return nil
}

func unmount(dst string) error {
	if out, err := exec.Command("umount", dst).CombinedOutput(); err != nil {
		return fmt.Errorf("issues while unmounting: %s", out)
	}

	return nil
}
