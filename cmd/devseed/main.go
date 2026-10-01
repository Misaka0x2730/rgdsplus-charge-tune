// Command devseed builds the fake console that `task run` uses (./devroot):
// a boot partition file, the live device tree of the charger, the model
// and firmware files, the battery and the two cards.
//
//	go run ./cmd/devseed                         # synthetic boot image
//	go run ./cmd/devseed -boot 03_boot.img       # a dump of the real one
//	go run ./cmd/devseed -running 2500,3000      # kernel "booted" with fast
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"rgdsplus-charge-tune/internal/bootimg"
	"rgdsplus-charge-tune/internal/platform"
)

func main() {
	root := flag.String("root", "devroot", "fake device root")
	boot := flag.String("boot", "", "boot partition dump to use instead of a synthetic image")
	running := flag.String("running", "2000,1500", "charge,input values the kernel booted with (mA)")
	battery := flag.Int("battery", 64, "battery level in percent")
	charger := flag.Bool("charger", false, "a charger is connected")
	lang := flag.Int("language", 2, "system language index (2 = English, 6 = Russian)")
	tf2 := flag.Bool("tf2", true, "TF2 is inserted")
	flag.Parse()

	p, err := platform.Load(platform.Options{Root: *root})
	if err != nil {
		fail(err)
	}
	write := func(devicePath string, data []byte) {
		path := p.FSPath(devicePath)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			fail(err)
		}
	}
	cell := func(v int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

	image := bootimg.Synth(bootimg.SynthOptions{TotalSize: int(p.BootSize)})
	if *boot != "" {
		if image, err = os.ReadFile(*boot); err != nil {
			fail(err)
		}
	}
	write(p.BootPartition, image)

	vals := strings.Split(*running, ",")
	if len(vals) != 2 {
		fail(fmt.Errorf("-running wants charge,input"))
	}
	chargeMA, err1 := strconv.Atoi(vals[0])
	inputMA, err2 := strconv.Atoi(vals[1])
	if err1 != nil || err2 != nil {
		fail(fmt.Errorf("-running wants numbers"))
	}
	node := p.DeviceTree + p.ChargerNode
	write(p.DeviceTree+"/compatible", []byte("rockchip,rk3568-deep-lp3-v10\x00rockchip,rk3568\x00"))
	write(node+"/compatible", []byte("rk817,charger\x00"))
	write(node+"/max_chrg_current", cell(chargeMA))
	write(node+"/max_input_current", cell(inputMA))
	write(node+"/max_chrg_voltage", cell(4400))

	write(p.BoardFile, []byte("RGdsplus\n"))
	write(p.FirmwareFile, []byte("20260915"))
	write(p.LanguageFile, []byte(strconv.Itoa(*lang)+"\n"))
	write(p.BatteryCapacityFile, []byte(strconv.Itoa(*battery)+"\n"))
	online := "0\n"
	if *charger {
		online = "1\n"
	}
	for _, f := range p.ChargerOnlineFiles {
		write(f, []byte(online))
	}
	write(p.LidFile, []byte("3\n"))
	if err := os.MkdirAll(p.FSPath("/mnt/mmc/Roms"), 0o755); err != nil {
		fail(err)
	}
	if *tf2 {
		if err := os.MkdirAll(p.FSPath("/mnt/sdcard/Roms"), 0o755); err != nil {
			fail(err)
		}
	} else {
		_ = os.RemoveAll(p.FSPath("/mnt/sdcard"))
	}
	fmt.Printf("fake console in %s: boot %d bytes, kernel booted with %d / %d mA, battery %d%%\n",
		*root, len(image), chargeMA, inputMA, *battery)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "devseed:", err)
	os.Exit(1)
}
