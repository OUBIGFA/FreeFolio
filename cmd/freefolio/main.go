// FreeFolio is a Markdown editor in the manner of Arc and Zen, drawn in native
// UI by MyGo.
//
//	go run ./cmd/freefolio
//	go run ./cmd/freefolio ask -space Work "why was the release delayed?"
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/OUBIGFA/FreeFolio/internal/app"
	"github.com/OUBIGFA/FreeFolio/internal/sysproxy"
	// The catalogs of mygo.json's i18n name the languages the installer and
	// the file properties are in; imported, they also give MyGo's own
	// controls and menus those languages.
	_ "github.com/OUBIGFA/FreeFolio/resources/locales"
	"github.com/egoist/mygo"
)

func main() {
	// Every HTTP client built on the default transport follows the proxy
	// environment, else the system proxy (ArcMD 14e92c1).
	sysproxy.Install()
	cfg, err := app.DefaultConfig()
	if err != nil {
		log.Fatal(err)
	}
	// FREEFOLIO_HOME keeps a development run's settings and spaces in one
	// folder, away from the user's own.
	if home := os.Getenv("FREEFOLIO_HOME"); home != "" {
		cfg = app.Config{ConfigDir: filepath.Join(home, "config"), DocumentsDir: filepath.Join(home, "spaces")}
	}
	if len(os.Args) > 1 && os.Args[1] == "ask" {
		os.Exit(ask(os.Args[2:], cfg, os.Stdout, os.Stderr))
	}
	// Claim the single-instance lock before reading configuration or space trees
	// so a second launch exits immediately instead of warming disk caches twice.
	mygo.App.SetName("FreeFolio")
	mygo.App.SetPath(mygo.PathUserData, cfg.ConfigDir)
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	cfg.DeferStartup = true
	cfg.InstanceLocked = true
	a, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
