//
// ubuntu-device-flash - Tool to download and flash devices with an Ubuntu Image
//                       based system
//
// Copyright (c) 2016 Canonical Ltd.
//
package main

var compatCanonicalPCamd64 = `
name: canonical-pc
gadget:
    branding:
        name:  amd64
        subname: generic

    hardware:
        bootloader: grub
        architecture: amd64
        partition-layout: minimal
        boot-assets:
            files:
                - path: grub.cfg
`

var compatCanonicalPCi386 = `
name: canonical-i386
gadget:
    branding:
        name:  i386
        subname: generic

    hardware:
        bootloader: grub
        architecture: amd64
        partition-layout: minimal
        boot-assets:
            files:
                - path: grub.cfg
`

var compatCanonicalPi2 = `
name: canonical-pi2
gadget:
  hardware:
    platform: bcm2836-rpi-2-b
    architecture: armhf
    partition-layout: minimal
    bootloader: u-boot
    boot-assets:
      files:
        - path: boot-assets/config.txt
        - path: boot-assets/cmdline.txt
        - path: boot-assets/uboot.bin
        - path: boot-assets/uboot.env
        - path: boot-assets/uEnv.txt
        - path: boot-assets/bcm2708-rpi-b.dtb
        - path: boot-assets/bcm2708-rpi-b-plus.dtb
        - path: boot-assets/bcm2709-rpi-2-b.dtb
        - path: boot-assets/bootcode.bin
        - path: boot-assets/COPYING.linux
        - path: boot-assets/fixup_cd.dat
        - path: boot-assets/fixup.dat
        - path: boot-assets/fixup_x.dat
        - path: boot-assets/LICENCE.broadcom
        - path: boot-assets/LICENSE.oracle
        - path: boot-assets/start_cd.elf
        - path: boot-assets/start.elf
        - path: boot-assets/start_x.elf
        - path: boot-assets/overlays.tgz
`

var compatCanonicalPi3 = `
name: pi3
gadget:
  hardware:
    platform: bcm2836-rpi-3-b
    architecture: armhf
    partition-layout: minimal
    bootloader: u-boot
    boot-assets:
      files:
        - path: boot-assets/bcm2708-rpi-b.dtb
        - path: boot-assets/bcm2708-rpi-b-plus.dtb
        - path: boot-assets/bcm2708-rpi-cm.dtb
        - path: boot-assets/bcm2709-rpi-2-b.dtb
        - path: boot-assets/bcm2710-rpi-3-b.dtb
        - path: boot-assets/bootcode.bin
        - path: boot-assets/cmdline.txt
        - path: boot-assets/config.txt
        - path: boot-assets/COPYING.linux
        - path: boot-assets/fixup_cd.dat
        - path: boot-assets/fixup.dat
        - path: boot-assets/fixup_db.dat
        - path: boot-assets/fixup_x.dat
        - path: boot-assets/LICENCE.broadcom
        - path: boot-assets/overlays.tgz
        - path: boot-assets/README
        - path: boot-assets/start_cd.elf
        - path: boot-assets/start_db.elf
        - path: boot-assets/start.elf
        - path: boot-assets/start_x.elf
        - path: boot-assets/uboot.bin
        - path: boot-assets/uboot.env
        - path: boot-assets/uboot.env.in
        - path: boot-assets/uboot.patch
`

var compatCanonicalDragon = `
name: canonical-dragon
gadget:
  branding:
    name: Dragonboard
    subname: Dragonboard

  hardware:
    platform: msm8916-mtp
    architecture: arm64
    partition-layout: minimal
    bootloader: u-boot
    boot-assets:
      files:
          - path: uboot.env
      raw-files:
          - path: sbl1.mbn
            offset: 17408
          - path: rpm.mbn
            offset: 541696
          - path: tz.mbn
            offset: 1065984
          - path: hyp.mbn
            offset: 1590272
          - path: sec.dat
            offset: 2114560
          - path: sd_appsboot.mbn
            offset: 2130944
          - path: u-boot.img
            offset: 3179520
      raw-partitions:
          - name: sbl1
            size: 512
            pos: 34
            type: DEA0BA2C-CBDD-4805-B4F9-F428251C3E98
          - name: rpm
            size: 512
            type: 098DF793-D712-413D-9D4E-89D711772228
          - name: tz
            size: 512
            type: A053AA7F-40B8-4B1C-BA08-2F68AC71A4F4
          - name: hyp
            size: 512
            type: E1A6A689-0C8D-4CC6-B4E8-55A4320FBD8A
          - name: sec
            size: 16
            type: 303E6AC3-AF15-4C54-9E9B-D9A8FBECF401
          - name: aboot
            size: 1024
            type: 400FFDCD-22E0-47E7-9A23-F16ED9382388
          - name: boot
            size: 512
            type: 20117F86-E985-4357-B9EE-374BC1D8487D
`

var compatPlanoYaml = `
name: plano-amd64
version: 1.23
summary: AMD64 generic package
description: |
    This package contains a simple OEM snappy package for system configuration
type: gadget

slots:
    cloud-led:
        interface: bool-file
        path: /sys/class/gpio/gpio346/value
    rs232-port-1:
        interface: serial-port
        path: /dev/ttyS6
    rs422-485-port-2:
        interface: serial-port
        path: /dev/ttyS4
    rs485-port-3:
        interface: serial-port
        path: /dev/ttyS5
    rs485-port-4:
        interface: serial-port
        path: /dev/ttyS2
    rs232-port-5-iom:
        interface: serial-port
        path: /dev/ttyS7
    zigbee:
        interface: serial-port
        path: /dev/ttyACM0

config:
    ubuntu-core:
        autopilot: true

gadget:
    branding:
        name: plano
        subname: based on amd64-generic

    skip-ifup-provisioning: true

#    software:
#        built-in:
#            - webdm

    hardware:
        bootloader: grub
        architecture: amd64
        partition-layout: minimal
        boot-assets:
            files:
                - path: grub.cfg
                - path: fwupdate.efi
                  target: fwupdate.efi
                  dst: EFI/ubuntu/fwupdate.efi
                - path: fwupx64.efi
                  dst: EFI/ubuntu/fwupx64.efi
                - path: shimx64.efi
                  dst: EFI/ubuntu/shimx64.efi
`
