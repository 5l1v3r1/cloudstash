package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/paddlesteamer/cloudstash/internal/common"
	"github.com/paddlesteamer/cloudstash/internal/config"
	"github.com/paddlesteamer/cloudstash/internal/crypto"
	"github.com/paddlesteamer/cloudstash/internal/drive"
	"github.com/paddlesteamer/cloudstash/internal/fs"
	"github.com/paddlesteamer/cloudstash/internal/manager"
	"github.com/paddlesteamer/go-fuse-c/fuse"
	"golang.org/x/crypto/ssh/terminal"

	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetLevel(log.DebugLevel)

	cfgDir, mntDir := parseFlags()

	// read existing or create new configuration file
	cfg, err := configure(cfgDir, mntDir)
	if err != nil {
		log.Errorf("configuration error: %v", err)
		return
	}

	// create mount directory
	if err := os.MkdirAll(cfg.MountPoint, 0755); err != nil {
		log.Errorf("could not create mount directory: %v", err)
		return
	}

	log.Infof("mount point: %s", cfg.MountPoint)

	drives, err := collectDrives(cfg)
	if err != nil {
		log.Errorf("couldn't collect drives: %v", err)
		return
	}

	dbDrv, err := findDBDrive(drives)
	if err != nil && err != common.ErrNotFound {
		log.Errorf("couldn't search for db file: %v", err)
		return
	}

	cipher := crypto.NewCipher(cfg.EncryptionKey)

	m, err := manager.NewManager(drives, dbDrv, cipher)
	if err != nil {
		log.Errorf("couldn't initialize manager: %v", err)
		return
	}
	defer m.Clean()

	// unmount when SIGINT, SIGTERM or SIGQUIT is received
	signalCh := make(chan os.Signal, 1)
	wg := sync.WaitGroup{}
	wg.Add(1)

	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	go handleSignal(signalCh, &wg, cfg.MountPoint)

	// mount the filesystem
	fs := fs.NewCloudStashFs(m)
	fuse.MountAndRun([]string{os.Args[0], cfg.MountPoint}, fs)

	// wait for signal handler to return
	wg.Wait()
}

// parseFlags parses the command-line flags.
func parseFlags() (cfgDir, mntDir string) {
	flag.StringVar(&cfgDir, "c", "", "Application config directory, optional.")
	flag.StringVar(&mntDir, "m", "", "Application mount directory, optional.")
	flag.Parse()

	return cfgDir, mntDir
}

func configure(cfgDir, mntDir string) (cfg *config.Cfg, err error) {
	if config.DoesConfigExist(cfgDir) {
		return config.ReadConfig(cfgDir)
	}

	fmt.Print("Enter encryption secret: ")
	secret, err := terminal.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return nil, fmt.Errorf("could not read encryption secret from terminal")
	}

	return config.NewConfig(cfgDir, mntDir, crypto.DeriveKey(secret))
}

// collectDrives returns a slice of clients for each enabled drive.
func collectDrives(cfg *config.Cfg) ([]drive.Drive, error) {
	drives := []drive.Drive{}

	if cfg.Dropbox != nil {
		dbox := drive.NewDropboxClient(cfg.Dropbox)
		drives = append(drives, dbox)
	}

	if cfg.GDrive != nil {
		gdrive, err := drive.NewGDriveClient(cfg.GDrive)
		if err != nil {
			return nil, fmt.Errorf("couldn't create gdrive client: %v", err)
		}

		drives = append(drives, gdrive)
	}

	return drives, nil
}

// findDBDrive searches for database file in drives and returns it if found
// returns common.ErrNotFound if not found
func findDBDrive(drives []drive.Drive) (drive.Drive, error) {
	for _, drv := range drives {
		if _, err := drv.GetFileMetadata(common.DatabaseFileName); err != nil {
			if err == common.ErrNotFound {
				continue
			}

			return nil, fmt.Errorf("couldn't get database file metadata from %s: %v",
				drv.GetProviderName(), err)
		}

		return drv, nil
	}

	return nil, common.ErrNotFound
}

func handleSignal(ch chan os.Signal, wg *sync.WaitGroup, mountpoint string) {
	defer wg.Done()

	_ = <-ch

	done := make(chan bool)
	umount(mountpoint, done)

	for {
		time.Sleep(5 * time.Second)

		select {
		case _ = <-done:
			return
		default:
			log.Warning("mounted device appears to be busy...")
			continue
		}
	}

}

func umount(mountpoint string, ch chan bool) {
	fuse.UMount(mountpoint)

	ch <- true
}
