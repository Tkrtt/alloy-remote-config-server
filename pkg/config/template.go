package config

import (
	"html/template"
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"
	"time"
	"github.com/fsnotify/fsnotify"
)

var (
	templates              = make(map[string]*template.Template)
	globalStorage *Storage = nil
	configFolder           = "conf" 
)

func LoadTemplates(path string) error {
	configFolder = path 
	files, err := filepath.Glob(filepath.Join(path, "*.conf.tmpl"))
	if err != nil {
		return err
	}

	for _, file := range files {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			return err
		}
		fullName := filepath.Base(file)
		tmpl, err := template.New(fullName).Parse(string(content))
		if err != nil {
			return err
		}
		trimmedName := strings.TrimSuffix(fullName, ".conf.tmpl")
		templates[trimmedName] = tmpl
	}

	return nil
}

// StartTemplateWatcher watches the config folder for changes and reloads templates
func StartTemplateWatcher(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Error creating file watcher: %v", err)
		return
	}
	defer watcher.Close()

	// Add the config folder to be watched
	err = watcher.Add(path)
	if err != nil {
		log.Printf("Error adding folder to watcher: %v", err)
		return
	}

	log.Printf("Watching directory %s for template changes", path)

	// Watch for events
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Only reload if it's a create, write, or remove event
			if event.Op&fsnotify.Create == fsnotify.Create ||
				event.Op&fsnotify.Write == fsnotify.Write ||
				event.Op&fsnotify.Remove == fsnotify.Remove {

				// Check if the file is a .conf.tmpl file
				if strings.HasSuffix(event.Name, ".conf.tmpl") {
					log.Printf("Template file changed: %s, reloading templates...", event.Name)
					if err := LoadTemplates(path); err != nil {
						log.Printf("Error reloading templates: %v", err)
					} else {
						log.Printf("Templates reloaded successfully")
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// StartTemplateWatcherWithInterval provides an alternative reload mechanism using polling
func StartTemplateWatcherWithInterval(path string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if templates need to be reloaded based on file modification times
			if err := LoadTemplates(path); err != nil {
				log.Printf("Error reloading templates: %v", err)
			}
		}
	}
}
