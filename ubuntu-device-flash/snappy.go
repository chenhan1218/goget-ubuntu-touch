//
// ubuntu-device-flash - Tool to download and flash devices with an Ubuntu Image
//                       based system
//
// Copyright (c) 2015 Canonical Ltd.
//
// Written by Sergio Schvezov <sergio.schvezov@canonical.com>
//
package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/provisioning"
	"github.com/ubuntu-core/snappy/release"
	"github.com/ubuntu-core/snappy/snap"
	// needed so that we register the squashfs format
	_ "github.com/ubuntu-core/snappy/snap/squashfs"
	"github.com/ubuntu-core/snappy/snappy"

	"gopkg.in/yaml.v2"
	"launchpad.net/goget-ubuntu-touch/diskimage"
	"launchpad.net/goget-ubuntu-touch/sysutils"
	"launchpad.net/goget-ubuntu-touch/ubuntuimage"
)

type imageFlavor string

const (
	minSizePersonal = 10
	minSizeCore     = 4
)

const (
	rootSizePersonal = 4096
	rootSizeCore     = 1024
)

const (
	flavorPersonal imageFlavor = "personal"
	flavorCore     imageFlavor = "core"
)

func (f imageFlavor) Channel() string {
	return fmt.Sprintf("ubuntu-%s", f)
}

func (f imageFlavor) minSize() int64 {
	switch f {
	case flavorPersonal:
		return minSizePersonal
	case flavorCore:
		return minSizeCore
	default:
		panic("invalid flavor")
	}
}

func (f imageFlavor) rootSize() int {
	switch f {
	case flavorPersonal:
		return rootSizePersonal
	case flavorCore:
		return rootSizeCore
	default:
		panic("invalid flavor")
	}
}

// Snapper holds common options applicable to snappy based images.
type Snapper struct {
	Channel string `long:"channel" description:"Specify the channel to use" default:"stable"`
	Output  string `long:"output" short:"o" description:"Name of the image file to create" required:"true"`
	Gadget  string `long:"gadget" description:"The snappy gadget package to base the image out of" default:"generic-amd64"`
	StoreID string `long:"store" description:"Set an alternate store id."`
	OS      string `long:"os" description:"path to the OS snap."`
	Kernel  string `long:"kernel" description:"path to the kernel snap."`

	Development struct {
		Install       []string `long:"install" description:"Install additional packages (can be called multiple times)"`
		DevicePart    string   `long:"device-part" description:"Specify a local device part to override the one from the server"`
		DeveloperMode bool     `long:"developer-mode" description:"Finds the latest public key in your ~/.ssh and sets it up using cloud-init"`
	} `group:"Development"`

	Positional struct {
		Release string `positional-arg-name:"release" description:"The release to base the image out of (16 or rolling)" required:"true"`
	} `positional-args:"yes" required:"yes"`

	img             diskimage.CoreImage
	hardware        diskimage.HardwareDescription
	gadget          diskimage.GadgetDescription
	stagingRootPath string

	size int64

	flavor imageFlavor
	device string

	customizationFunc []func() error
}

func (s Snapper) sanityCheck() error {
	// we don't want to overwrite the output, people might get angry :-)
	if osutil.FileExists(s.Output) {
		return fmt.Errorf("Giving up, the desired target output file %#v already exists", s.Output)
	}

	if s.size < s.flavor.minSize() {
		return fmt.Errorf("minimum size for %s is %d", s.flavor, s.flavor.minSize())
	}

	if syscall.Getuid() != 0 {
		return errors.New("command requires sudo/pkexec (root)")
	}
	if s.Positional.Release == "15.04" {
		return errors.New("building 15.04 core images is no longer supported. Please use the ppa:snappy-dev/tools 15.04 version of this tool")
	}

	// ensure we error when running on e.g. 14.04 with a sensible
	// error message instead of super strange error later
	if !osutil.FileExists("/bin/systemctl") {
		return errors.New("need '/bin/systemctl to work")
	}

	return nil
}

func (s *Snapper) systemImage() (*ubuntuimage.Image, error) {
	channels, err := ubuntuimage.NewChannels(globalArgs.Server)
	if err != nil {
		return nil, err
	}

	channel := systemImageChannel(s.flavor.Channel(), s.Positional.Release, s.Channel)
	// TODO: remove once azure channel is gone
	if s.device == "" {
		s.device = systemImageDeviceChannel(s.gadget.Architecture())
	}

	deviceChannel, err := channels.GetDeviceChannel(globalArgs.Server, channel, s.device)
	if err != nil {
		return nil, err
	}

	systemImage, err := getImage(deviceChannel)
	if err != nil {
		return nil, err
	}

	// avoid passing more args to setup()
	globalArgs.Revision = systemImage.Version

	return &systemImage, nil
}

func systemdEnable(serviceName string) error {
	fmt.Printf("Enabling systemd unit %s\n", serviceName)

	servicesSystemdTarget := "multi-user.target"
	snapServicesDir := "/etc/systemd/system"

	enableSymlink := filepath.Join(dirs.GlobalRootDir, snapServicesDir, servicesSystemdTarget+".wants", serviceName)

	serviceFilename := filepath.Join(dirs.GlobalRootDir, snapServicesDir, serviceName)

	return os.Symlink(serviceFilename[len(dirs.GlobalRootDir):], enableSymlink)

}

func (s *Snapper) installFlags() snappy.InstallFlags {
	flags := snappy.InhibitHooks | snappy.AllowGadget

	if s.Development.DeveloperMode {
		flags |= snappy.AllowUnauthenticated
	}

	return flags
}

func (s *Snapper) install(systemPath string) error {
	dirs.SetRootDir(systemPath)
	defer dirs.SetRootDir("/")

	flags := s.installFlags()
	gadgetSoftware := s.gadget.Gadget.Software
	packageCount := len(s.Development.Install) + len(gadgetSoftware.BuiltIn) + len(gadgetSoftware.Preinstalled) + 3
	if s.Gadget != "" {
		packageCount++
	}

	packageQueue := make([]string, 0, packageCount)
	if s.Gadget != "" {
		packageQueue = append(packageQueue, s.Gadget)
	}
	if s.OS != "" && s.Kernel != "" {
		packageQueue = append(packageQueue, s.Kernel)
		packageQueue = append(packageQueue, s.OS)
	}
	packageQueue = append(packageQueue, gadgetSoftware.BuiltIn...)
	packageQueue = append(packageQueue, gadgetSoftware.Preinstalled...)
	packageQueue = append(packageQueue, s.Development.Install...)

	for _, snap := range packageQueue {
		fmt.Println("Installing", snap)

		pb := progress.NewTextProgress()
		name := snap
		if _, err := snappy.Install(name, s.Channel, flags, pb); err != nil {
			return fmt.Errorf("failed to install %q from %q: %s", name, s.Channel, err)
		}
	}

	// set the bootvars for kernel/os snaps, the latest snappy is
	// not activating the snaps on install anymore (with inhibit)
	// so we need to work around that here (only on first boot)
	//
	// there is also no mounted os/kernel snap in the systemPath
	// all we have here is the blobs
	if s.OS != "" && s.Kernel != "" {
		bootloader, err := partition.FindBootloader()
		if err != nil {
			return fmt.Errorf("can not set kernel/os bootvars: %s", err)
		}

		snaps, _ := filepath.Glob(filepath.Join(dirs.SnapBlobDir, "*.snap"))
		for _, fullname := range snaps {
			bootvar := ""
			bootvar2 := ""

			// detect type
			snapFile, err := snap.Open(fullname)
			if err != nil {
				return fmt.Errorf("can not read %v", fullname)
			}
			info, err := snapFile.Info()
			if err != nil {
				return fmt.Errorf("can not get info for %v", fullname)
			}
			switch info.Type {
			case snap.TypeOS:
				bootvar = "snappy_os"
				bootvar2 = "snappy_good_os"
			case snap.TypeKernel:
				bootvar = "snappy_kernel"
				bootvar2 = "snappy_good_kernel"
			}

			name := filepath.Base(fullname)
			for _, b := range []string{bootvar, bootvar2} {
				if b != "" {
					if err := bootloader.SetBootVar(b, name); err != nil {
						return err
					}
				}
			}
		}

		// HORRIBLE, snappy.Install() will check if running
		// on a grub system based on the gadget snap and if
		// it is grub it will not extract the kernel/os
		//
		// HOWEVER this won't work in u-d-f because there
		// is no current symlink so kernel.go always unpacks
		// the kernel. undo this here
		if s.gadget.Gadget.Hardware.Bootloader == "grub" {
			dirs, _ := filepath.Glob(filepath.Join(s.img.Boot(), "/EFI/ubuntu/grub/*.snap"))
			for _, d := range dirs {
				fmt.Printf("Removing unneeded: %s\n", d)
				if err := os.RemoveAll(d); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (s *Snapper) extractGadget(gadgetPackage string) error {
	if gadgetPackage == "" {
		return nil
	}

	tempDir, err := ioutil.TempDir("", "gadget")
	if err != nil {
		return err
	}

	// we need to fix the permissions for tempdir to  be seteuid friendly
	if err := os.Chmod(tempDir, 0755); err != nil {
		return err
	}

	s.stagingRootPath = tempDir
	os.MkdirAll(filepath.Join(tempDir, "/snap"), 0755)

	dirs.SetRootDir(tempDir)
	defer dirs.SetRootDir("/")
	release.Override(release.Release{
		Flavor: string(s.flavor),
		Series: s.Positional.Release,
	})

	// we need to download and extract the squashfs snap
	downloadedSnap := gadgetPackage
	if !osutil.FileExists(gadgetPackage) {
		repo := snappy.NewConfiguredUbuntuStoreSnapRepository()
		snap, err := repo.Snap(gadgetPackage, s.Channel, nil)
		if err != nil {
			return fmt.Errorf("expected a gadget snaps: %s", err)
		}

		pb := progress.NewTextProgress()
		downloadedSnap, err = repo.Download(snap, pb, nil)
		if err != nil {
			return err
		}
	}

	// the fake snap needs to be in an expected location so that
	// s.loadGadget() is happy
	fakeGadgetDir := filepath.Join(tempDir, "/gadget/fake-gadget/1.0-fake/")
	if err := os.MkdirAll(fakeGadgetDir, 0755); err != nil {
		return err
	}
	cmd := exec.Command("unsquashfs", "-i", "-f", "-d", fakeGadgetDir, downloadedSnap)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("snap unpack failed with: %v (%v)", err, string(output))
	} else {
		println(string(output))
	}

	if err := s.loadGadget(tempDir); err != nil {
		return err
	}

	return nil
}

func (s *Snapper) loadGadget(systemPath string) error {
	pkgs, err := filepath.Glob(filepath.Join(systemPath, "/gadget/*/*/meta/snap.yaml"))
	if err != nil {
		return err
	}

	// checking for len(pkgs) > 2 due to the 'current' symlink
	if len(pkgs) == 0 {
		return errors.New("no gadget package found")
	} else if len(pkgs) > 2 || err != nil {
		return errors.New("too many gadget packages installed")
	}

	f, err := ioutil.ReadFile(pkgs[0])
	if err != nil {
		return errors.New("failed to read gadget yaml")
	}

	var gadget diskimage.GadgetDescription
	if err := yaml.Unmarshal([]byte(f), &gadget); err != nil {
		return errors.New("cannot decode gadget yaml")
	}
	s.gadget = gadget
	s.gadget.SetRoot(systemPath)

	// ensure we can download and install snaps
	arch.SetArchitecture(arch.ArchitectureType(s.gadget.Architecture()))

	return nil
}

// Creates a YAML file inside the image that contains metadata relating
// to the installation.
func (s Snapper) writeInstallYaml(bootMountpoint string) error {
	selfPath, err := exec.LookPath(os.Args[0])
	if err != nil {
		return err
	}

	bootDir := ""

	switch s.gadget.Gadget.Hardware.Bootloader {
	// Running systems use a bindmount for /boot/grub, but
	// since the system isn't booted, create the file in the
	// real location.
	case "grub":
		bootDir = "/EFI/ubuntu/grub"
	}

	installYamlFilePath := filepath.Join(bootMountpoint, bootDir, provisioning.InstallYamlFile)

	i := provisioning.InstallYaml{
		InstallMeta: provisioning.InstallMeta{
			Timestamp: time.Now(),
		},
		InstallTool: provisioning.InstallTool{
			Name: filepath.Base(selfPath),
			Path: selfPath,
			// FIXME: we don't know our own version yet :)
			// Version: "???",
		},
		InstallOptions: provisioning.InstallOptions{
			Size:          s.size,
			SizeUnit:      "GB",
			Output:        s.Output,
			Channel:       s.Channel,
			DevicePart:    s.Development.DevicePart,
			Gadget:        s.Gadget,
			OS:            s.OS,
			Kernel:        s.Kernel,
			DeveloperMode: s.Development.DeveloperMode,
		},
	}

	data, err := yaml.Marshal(&i)
	if err != nil {
		return err
	}

	// the file isn't supposed to be modified, hence r/o.
	return ioutil.WriteFile(installYamlFilePath, data, 0444)
}

func extractHWDescription(path string) (hw diskimage.HardwareDescription, err error) {
	// hack to circumvent https://code.google.com/p/go/issues/detail?id=1435
	if syscall.Getuid() == 0 {
		runtime.GOMAXPROCS(1)
		runtime.LockOSThread()

		if err := sysutils.DropPrivs(); err != nil {
			return hw, err
		}
	}

	printOut("Searching for hardware.yaml in device part")
	tmpdir, err := ioutil.TempDir("", "hardware")
	if err != nil {
		return hw, errors.New("cannot create tempdir to extract hardware.yaml from device part")
	}
	defer os.RemoveAll(tmpdir)

	if out, err := exec.Command("tar", "xf", path, "-C", tmpdir, "hardware.yaml").CombinedOutput(); err != nil {
		return hw, fmt.Errorf("failed to extract a hardware.yaml from the device part: %s", out)
	}

	data, err := ioutil.ReadFile(filepath.Join(tmpdir, "hardware.yaml"))
	if err != nil {
		return hw, err
	}

	err = yaml.Unmarshal([]byte(data), &hw)

	return hw, err
}

func (s *Snapper) bindMount(d string) (string, error) {
	src := filepath.Join(s.img.Writable(), "system-data", d)
	dst := filepath.Join(s.img.System(), d)

	if err := os.MkdirAll(src, 0755); err != nil {
		return "", err
	}
	cmd := exec.Command("mount", "--bind", src, dst)
	if o, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("bind mount failed for %s to %s with: %s %v ", src, dst, err, string(o))
	}
	return dst, nil
}

func (s *Snapper) downloadOS(osPackage string) (string, error) {
	if osPackage == "" {
		return "", nil
	}
	// if its pointing to a local file, just return that
	if _, err := os.Stat(osPackage); err == nil {
		return osPackage, nil
	}

	release.Override(release.Release{
		Flavor: string(s.flavor),
		Series: s.Positional.Release,
	})
	m := snappy.NewConfiguredUbuntuStoreSnapRepository()
	snap, err := m.Snap(osPackage, s.Channel, nil)
	if err != nil {
		return "", fmt.Errorf("failed to find os snap: %s", err)
	}
	pb := progress.NewTextProgress()
	path, err := m.Download(snap, pb, nil)
	if err != nil {
		return "", err
	}

	return path, nil
}

func (s *Snapper) setup(systemImageFiles []Files) error {
	printOut("Mounting...")
	if err := s.img.Mount(); err != nil {
		return err
	}
	defer func() {
		printOut("Unmounting...")
		if err := s.img.Unmount(); err != nil {
			fmt.Println("WARNING: unexpected issue:", err)
		}
	}()

	printOut("Provisioning...")
	for i := range systemImageFiles {
		if out, err := exec.Command("fakeroot", "tar", "--numeric-owner", "-axvf", systemImageFiles[i].FilePath, "-C", s.img.BaseMount()).CombinedOutput(); err != nil {
			printOut(string(out))
			return fmt.Errorf("issues while extracting: %s", out)
		}
	}

	systemPath := s.img.System()

	// setup a fake system
	if s.gadget.PartitionLayout() == "minimal" {
		if err := os.MkdirAll(systemPath, 0755); err != nil {
			return err
		}

		// this is a bit terrible, we need to download the OS
		// mount it, "sync dirs" (see below) and then we
		// will need to download it again to install it properly
		osSnap, err := s.downloadOS(s.OS)
		if err != nil {
			return err
		}

		// mount os snap
		cmd := exec.Command("mount", osSnap, systemPath)
		if o, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("os snap mount failed with: %s %v ", err, string(o))
		}
		defer exec.Command("umount", systemPath).Run()

		// we need to do what "writable-paths" normally does on
		// boot for etc/systemd/system, i.e. copy all the stuff
		// from the os into the writable partition. normally
		// this is the job of the initrd, however it won't touch
		// the dir if there are files in there already. and a
		// kernel/os install will create auto-mount units in there
		src := filepath.Join(systemPath, "etc", "systemd", "system")
		dst := filepath.Join(s.img.Writable(), "system-data", "etc", "systemd")
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		cmd = exec.Command("cp", "-a", src, dst)
		if o, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("copy failed: %s %s", err, o)
		}

		// bind mount all relevant dirs
		for _, d := range []string{"snap", "var/snap", "var/lib/snapd", "etc/systemd/system/", "tmp"} {
			dst, err := s.bindMount(d)
			if err != nil {
				return err
			}
			defer exec.Command("umount", dst).Run()
		}

		// bind mount /boot/efi
		dst = filepath.Join(systemPath, "/boot/efi")
		cmd = exec.Command("mount", "--bind", s.img.Boot(), dst)
		if o, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("boot bind mount failed with: %s %v ", err, string(o))
		}
		defer exec.Command("umount", dst).Run()
		switch s.gadget.Gadget.Hardware.Bootloader {
		case "grub":
			// grub needs this
			grubUbuntu := filepath.Join(s.img.Boot(), "EFI/ubuntu/grub")
			os.MkdirAll(grubUbuntu, 0755)

			// and /boot/grub
			src = grubUbuntu
			dst = filepath.Join(systemPath, "/boot/grub")
			cmd = exec.Command("mount", "--bind", src, dst)
			if o, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("boot/ubuntu bind mount failed with: %s %v ", err, string(o))
			}
			defer exec.Command("umount", dst).Run()

			// TERRIBLE but we need a /boot/grub/grub.cfg so that
			//          the kernel and os snap can be installed
			glob, err := filepath.Glob(filepath.Join(s.stagingRootPath, "gadget", "*", "*", "grub.cfg"))
			if err != nil {
				return fmt.Errorf("grub.cfg glob failed: %s", err)
			}
			if len(glob) != 1 {
				return fmt.Errorf("can not find a valid grub.cfg, found %v instead", len(glob))
			}
			gadgetGrubCfg := glob[0]
			cmd = exec.Command("cp", gadgetGrubCfg, grubUbuntu)
			o, err := cmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to copy %s %s", err, o)
			}
		case "u-boot":
			src = s.img.Boot()
			dst = filepath.Join(systemPath, "/boot/uboot")
			cmd = exec.Command("mount", "--bind", src, dst)
			if o, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("boot/ubuntu bind mount failed with: %s %v ", err, string(o))
			}
			defer exec.Command("umount", dst).Run()
		}
	}

	if err := s.img.SetupBoot(); err != nil {
		return err
	}

	if err := s.install(systemPath); err != nil {
		return err
	}

	for i := range s.customizationFunc {
		if err := s.customizationFunc[i](); err != nil {
			return err
		}
	}

	return s.writeInstallYaml(s.img.Boot())
}

// deploy orchestrates the priviledged part of the setup
func (s *Snapper) deploy(systemImageFiles []Files) error {
	// hack to circumvent https://code.google.com/p/go/issues/detail?id=1435
	runtime.GOMAXPROCS(1)
	runtime.LockOSThread()
	if err := sysutils.EscalatePrivs(); err != nil {
		return err
	}
	defer sysutils.DropPrivs()

	printOut("Formatting...")
	if err := s.img.Format(); err != nil {
		return err
	}

	if err := s.setup(systemImageFiles); err != nil {
		return err
	}

	return nil
}

func (s Snapper) printSummary() {
	fmt.Println("New image complete")
	fmt.Println("Summary:")
	fmt.Println(" Output:", s.Output)
	fmt.Println(" Architecture:", s.gadget.Architecture())
	fmt.Println(" Channel:", s.Channel)
	fmt.Println(" Version:", globalArgs.Revision)
}

func (s *Snapper) getSystemImage() ([]Files, error) {
	var devicePart string
	if s.Development.DevicePart != "" {
		p, err := expandFile(s.Development.DevicePart)
		if err != nil {
			return nil, err
		}

		fmt.Println("Using a custom OS or Kernel part will prevent updates for these components")

		devicePart = p
	}

	fmt.Println("Fetching information from server...")
	systemImage, err := s.systemImage()
	if err != nil {
		return nil, err
	}

	filesChan := make(chan Files, len(systemImage.Files))
	sigFiles := ubuntuimage.GetGPGFiles()

	fmt.Println("Downloading and setting up...")

	go func() {
		sigFilesChan := make(chan Files, len(sigFiles))
		defer close(sigFilesChan)

		for _, f := range sigFiles {
			bitDownloader(f, sigFilesChan, globalArgs.Server, cacheDir)
		}
	}()

	filePaths := make([]Files, 0, len(systemImage.Files))
	hwChan := make(chan diskimage.HardwareDescription)

	go func() {
		for i := 0; i < len(systemImage.Files); i++ {
			f := <-filesChan

			if isDevicePart(f.FilePath) {
				devicePart = f.FilePath

				if hardware, err := extractHWDescription(f.FilePath); err != nil {
					fmt.Println("Failed to read harware.yaml from device part, provisioning may fail:", err)
				} else {
					hwChan <- hardware
				}
			}

			printOut("Download finished for", f.FilePath)
			filePaths = append(filePaths, f)
		}
		close(hwChan)
		close(filesChan)
	}()

	for _, f := range systemImage.Files {
		if devicePart != "" && isDevicePart(f.Path) {
			printOut("Using a custom device tarball")
			filesChan <- Files{FilePath: devicePart}
		} else {
			go bitDownloader(f, filesChan, globalArgs.Server, cacheDir)
		}
	}

	s.hardware = <-hwChan

	return filePaths, nil
}

func (s *Snapper) create() (err error) {
	if err := s.sanityCheck(); err != nil {
		return err
	}

	if s.StoreID != "" {
		fmt.Println("Setting store id to", s.StoreID)
		os.Setenv("UBUNTU_STORE_ID", s.StoreID)
	}

	fmt.Println("Determining gadget configuration")
	if err := s.extractGadget(s.Gadget); err != nil {
		return err
	}
	defer os.RemoveAll(s.stagingRootPath)

	// hack to circumvent https://code.google.com/p/go/issues/detail?id=1435
	runtime.GOMAXPROCS(1)
	runtime.LockOSThread()
	if err := sysutils.DropPrivs(); err != nil {
		return err
	}

	systemImageFiles := []Files{}
	switch s.gadget.Gadget.Hardware.PartitionLayout {
	case "minimal":
		if s.OS == "" && s.Kernel == "" {
			return errors.New("kernel and os have to be specified to support partition-layout: minimal")
		}
	}

	switch s.gadget.Gadget.Hardware.Bootloader {
	case "grub":
		legacy := isLegacy(s.Positional.Release, s.Channel, globalArgs.Revision)
		if legacy {
			printOut("Using legacy setup")
		}

		s.img = diskimage.NewCoreGrubImage(s.Output, s.size, s.flavor.rootSize(), s.hardware, s.gadget, legacy, "gpt")
	case "u-boot":
		label := "msdos"
		if s.gadget.Architecture() == archArm64 {
			label = "gpt"
		}
		s.img = diskimage.NewCoreUBootImage(s.Output, s.size, s.flavor.rootSize(), s.hardware, s.gadget, label)
	default:
		return errors.New("no hardware description in Gadget snap")
	}

	printOut("Partitioning...")
	if err := s.img.Partition(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(s.Output)
		}
	}()

	// Handle SIGINT and SIGTERM.
	go func() {
		ch := make(chan os.Signal)
		signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)

		for sig := range ch {
			printOut("Received", sig, "... ignoring")
		}
	}()

	// Execute the following code with escalated privs and drop them when done
	if err := s.deploy(systemImageFiles); err != nil {
		return err
	}

	if err := s.img.FlashExtra(); err != nil {
		return err
	}

	s.printSummary()

	return nil
}

func isLegacy(release, channel string, revision int) bool {
	if release != "15.04" {
		return false
	}

	switch channel {
	case "edge":
		return revision <= 149
	case "alpha":
		return revision <= 9
	case "stable":
		return revision <= 4
	}

	return false
}
