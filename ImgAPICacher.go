package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/* Default values */
const (
	Local                       Mode   = "local"
	Remote                      Mode   = "remote"
	DefaultConfigFileName       string = "config.json"
	ConfigDefaultListenPort     int    = 8080
	ConfigDefaultCacheFolder    string = "cache"
	ConfigDefaultCacheTmpFolder string = "tmp"
	ConfigDefaultUpdateInterval int64  = 3
	ConfigDefaultMaxCacheSize   int    = 0 // 0 = unlimited
	ConfigDefaultImageQuality   int    = 60
	ConfigDefaultRemote1        string = "https://api.nyan.xyz/httpapi/sexphoto"
	ConfigDefaultRemote2        string = "https://loliapi.com/acg"
)

/* Custom types/structs */
type Mode string
type Config struct {
	ListenPort     int
	LogFileName    string
	Mode           Mode
	CacheFolder    string
	CacheTmpFolder string
	UpdateInterval int64
	MaxCacheSize   int
	ImageQuality   int
	Remotes        []string
}

/* Helper functions */

// Function for standardize config reading/creating
func newConfig(config Config) Config {
	// Create new config
	newConfig := Config{
		ListenPort:     ConfigDefaultListenPort,
		Mode:           Remote,
		CacheFolder:    ConfigDefaultCacheFolder,
		CacheTmpFolder: ConfigDefaultCacheTmpFolder,
		UpdateInterval: ConfigDefaultUpdateInterval,
		MaxCacheSize:   ConfigDefaultMaxCacheSize,
		ImageQuality:   ConfigDefaultImageQuality,
		Remotes:        []string{ConfigDefaultRemote1, ConfigDefaultRemote2},
	}

	// Check if any config values are invalid and replace them with default values
	if config.ListenPort >= 1024 && config.ListenPort <= 65535 {
		newConfig.ListenPort = config.ListenPort
	} else {
		log.Println("Warning: ListenPort out of range, using default value " + strconv.Itoa(ConfigDefaultListenPort))
	}
	if config.LogFileName != "" {
		newConfig.LogFileName = config.LogFileName
	} else {
		log.Println("Warning: LogFileName is empty, disabling log file")
	}
	if config.Mode == Local || config.Mode == Remote {
		newConfig.Mode = config.Mode
	} else {
		log.Println("Warning: Mode invalid, using default value " + Remote)
	}
	if config.CacheFolder != "" {
		newConfig.CacheFolder = config.CacheFolder
	} else {
		log.Println("Warning: CacheFolder invalid, using default value " + ConfigDefaultCacheFolder)
	}
	if config.CacheTmpFolder != "" {
		newConfig.CacheTmpFolder = config.CacheTmpFolder
	} else {
		log.Println("Warning: CacheTmpFolder invalid, using default value " + ConfigDefaultCacheTmpFolder)
	}
	if config.UpdateInterval > 0 {
		newConfig.UpdateInterval = config.UpdateInterval
	} else {
		log.Println("Warning: UpdateInterval out of range, using default value " + strconv.FormatInt(ConfigDefaultUpdateInterval, 10))
	}
	if config.MaxCacheSize >= 0 {
		newConfig.MaxCacheSize = config.MaxCacheSize
	} else {
		log.Println("Warning: MaxCacheSize out of range, using default value " + strconv.Itoa(ConfigDefaultMaxCacheSize))
	}
	if config.ImageQuality > 0 {
		newConfig.ImageQuality = config.ImageQuality
	} else {
		log.Println("Warning: ImageQuality out of range, using default value " + strconv.Itoa(ConfigDefaultImageQuality))
	}
	if config.Remotes != nil {
		newConfig.Remotes = config.Remotes
	} else {
		log.Println("Warning: Remotes invalid, using default value [" + ConfigDefaultRemote1 + ", " + ConfigDefaultRemote2 + "]")
	}

	// Finished creating config
	return newConfig
}

// Function for reading config from file
func readConfig() Config {
	// Read config file
	file, err := ioutil.ReadFile(DefaultConfigFileName)
	if err != nil {
		log.Fatalln("Error:", err)
	}
	var config Config
	err = json.Unmarshal(file, &config)
	if err != nil {
		log.Fatalln("Error:", err)
	}
	return config
}

// Function for writing config to file
func writeConfig(config Config) {
	// Write config struct to json file
	file, _ := json.MarshalIndent(config, "", "\t")
	err := ioutil.WriteFile(DefaultConfigFileName, file, 0644)
	if err != nil {
		log.Println("Error:", err)
	}
}

// Function for general config reading/writing/creating
func getConfig() Config {
	// Reacd/Write/Create config file
	var config Config
	if _, err := os.Stat(DefaultConfigFileName); err == nil {
		log.Println("Config file found, reading...")
		config = newConfig(readConfig())
	} else if errors.Is(err, os.ErrNotExist) {
		// No config file, create one
		log.Println("No config file found, creating one...")
		config = newConfig(config)
	} else {
		log.Fatalln("Error:", err)
	}
	writeConfig(config)
	return config
}

// Function for reloading config file
func reloadConfig(w http.ResponseWriter, r *http.Request) {
	config = getConfig()
	log.Println("Reloaded config: \n", getConfigString(config))
	fmt.Fprintf(w, "Config reloaded")
}

// Function for converting config to pretty string
func getConfigString(config Config) string {
	configString, err := json.MarshalIndent(config, "", "\t")
	if err != nil {
		return fmt.Sprintf("%+v\n", config)
	} else {
		return string(configString)
	}
}

// Function for getting file extension from MIME type
func getExtension(contentType string) string {
	if contentType == "image/jpeg" {
		return "jpg"
	}
	if contentType == "image/png" {
		return "png"
	}
	return ""
}

// Function for extracting imgURL from a json response
func getImgURL(response string) string {
	// Use regex to extract image URL from http response
	response = strings.Replace(response, `\/`, "/", -1)
	pattern := regexp.MustCompile(`https?\:\/\/.+\.(?i)(jpg|jpeg|png)`)
	return pattern.FindString(response)
}

// Function for extracting image extension from a filename/URL
func getImgExtension(filename string) string {
	pattern := regexp.MustCompile(`.+\.(?i)(jpg|jpeg|png)$`)
	match := pattern.FindStringSubmatch(filename)
	if len(match) >= 2 {
		return strings.ToLower(match[1])
	}
	return ""
}

// Function for downloading file from URL to given local filename
func downloadFile(filename string, URL string) error {

	// Create the file
	out, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer out.Close()

	// Get the data
	resp, err := http.Get(URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Writer the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

// Function to compress image to given quality in config
func compressImage(data []byte) ([]byte, error) {
	imgSrc, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, err
	}
	newImg := image.NewRGBA(imgSrc.Bounds())
	draw.Draw(newImg, newImg.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(newImg, newImg.Bounds(), imgSrc, imgSrc.Bounds().Min, draw.Over)
	buf := bytes.Buffer{}
	err = jpeg.Encode(&buf, newImg, &jpeg.Options{Quality: config.ImageQuality})
	if err != nil {
		return data, err
	}
	if buf.Len() > len(data) {
		return data, nil
	}
	return buf.Bytes(), nil
}

// Function for detecting if a file is a valid and supported image
func isImage(filename string) bool {
	// Frist check if file extension is supported
	if getImgExtension(filename) == "" {
		return false
	}
	// Then check content type by opening and read it into buffer
	filename = config.CacheFolder + string(os.PathSeparator) + filename
	imageFile, err := os.Open(filename)
	if err != nil {
		log.Println("Error:", err)
		return false
	}
	defer imageFile.Close()
	// Only take the first 512 bytes of the file to check the content type
	buff := make([]byte, 512)
	if _, err = imageFile.Read(buff); err != nil {
		// File is not an image, return false
		return false
	}

	return true
}

// Function for retrieving image from remotes
func retrieveRemote(hostname string, w http.ResponseWriter, r *http.Request) {
	// Start retrieving process
	log.Println("--- Starting Remote Retrieval ---")
	// Update last update timestamp
	timestamp = time.Now().Unix()

	// Get a random remote from config.Remotes
	remote := config.Remotes[rand.Intn(len(config.Remotes))]
	log.Println("Retrieving remote: ", remote)

	// Send get request to remote
	response, err := http.Get(remote)
	if err != nil {
		log.Println("Error:", err)
		return
	}
	defer response.Body.Close()

	// Validate response status code
	if response.StatusCode != 200 && response.StatusCode != 302 && response.StatusCode != 301 {
		log.Println("Error:", errors.New("Invalid response status code "+strconv.Itoa(response.StatusCode)))
		return
	}

	// Get response content type and decide whether to extract image URL from response body
	var imgURL string
	contentType := response.Header.Get("Content-Type")
	extension := getExtension(contentType)
	if extension != "" {
		// Content type is an image, then we should directly download from this URL
		imgURL = remote
	} else {
		// Extract image URL from response body
		body, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Println("Error:", err)
			return
		}
		imgURL = getImgURL(string(body))
		extension = getImgExtension(imgURL)
	}
	log.Println("Retrieving from URL: ", imgURL)

	// Filename for uncompressed image
	filenameUncompressed := string(config.CacheFolder+string(os.PathSeparator)+config.CacheTmpFolder+string(os.PathSeparator)+strconv.FormatInt(time.Now().UnixNano(), 10)) + "." + extension
	// Check if cache folder and its tmp folder exists
	if _, err := os.Stat(config.CacheFolder); os.IsNotExist(err) {
		// Create cache folder
		log.Println("Creating cache folder: ", config.CacheFolder)
		err = os.Mkdir(config.CacheFolder, 0755)
		if err != nil {
			log.Fatalln("Error:", err)
			return
		}
	}
	if _, err := os.Stat(config.CacheFolder + string(os.PathSeparator) + config.CacheTmpFolder); os.IsNotExist(err) {
		// Create tmp folder for uncompressed images
		log.Println("Creating tmp folder: ", config.CacheFolder+string(os.PathSeparator)+config.CacheTmpFolder)
		err = os.Mkdir(config.CacheFolder+string(os.PathSeparator)+config.CacheTmpFolder, 0755)
		if err != nil {
			log.Fatalln("Error:", err)
			return
		}
	}

	// Download image to tmp folder
	log.Println("Downloading image to: ", filenameUncompressed)
	err = downloadFile(filenameUncompressed, imgURL)
	if err != nil {
		log.Println("Error:", err)
		return
	}

	// Read and compress image
	filenameCompressed := string(config.CacheFolder+string(os.PathSeparator)+strconv.FormatInt(time.Now().UnixNano(), 10)) + ".jpg"
	log.Println("Compressing image to: ", filenameCompressed)
	data, err := ioutil.ReadFile(filenameUncompressed)
	if err != nil {
		log.Println("Error:", err)
		return
	}
	// Save compressed image to cache folder
	data, err = compressImage(data)
	err = ioutil.WriteFile(filenameCompressed, data, 0644)
	if err != nil {
		log.Println("Error:", err)
		return
	}

	// Remove uncompressed image from tmp folder
	err = os.Remove(filenameUncompressed)
	if err != nil {
		log.Println("Error:", err)
		return
	} else {
		log.Println("Removed uncompressed image: ", filenameUncompressed)
	}

	// Check if current number of images have reached the MaxCacheSize limit
	if config.MaxCacheSize != 0 {
		files, err := ioutil.ReadDir(config.CacheFolder)
		if err != nil {
			log.Println("Error:", err)
			return
		} else {
			if len(files) >= config.MaxCacheSize {
				// Limit MaxCacheSize reached, change mode to local
				config.Mode = Local
				writeConfig(config)
				log.Println("Limit of MaxCacheSize (", config.MaxCacheSize, ") reached, switching mode to local")
			}
		}
	}

	// Serve image link
	fmt.Fprintf(w, "http://%s/%s", hostname, strings.Replace(filenameCompressed, "\\", "/", -1))
	log.Println("--- Finished Remote Retrieval ---")
}

/* Main functions */

// Global varable for storing config and timestamp (for recording last update time)
var config Config
var timestamp int64

// Function for handle general HTTP request
func handleRequest(w http.ResponseWriter, r *http.Request) {
	// Make sure only accept GET requests
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// If requesting favicon.ico, return 404
	if r.URL.Path == "/favicon.ico" {
		http.NotFound(w, r)
		return
	}

	// If requesting image in cache folder, return that image
	if strings.HasPrefix(r.URL.Path, "/"+config.CacheFolder+"/") {
		// Make sure the requesting filename is of one of supported extensions
		if getImgExtension(r.URL.Path) == "" {
			http.NotFound(w, r)
			return
		}

		// Get image from cache folder
		filepath := config.CacheFolder + string(os.PathSeparator) + r.URL.Path[len(config.CacheFolder)+1:]
		if _, err := os.Stat(filepath); err == nil {
			// Image exists, return it
			http.ServeFile(w, r, filepath)
			return
		} else {
			// Image doesn't exist, return 404
			http.NotFound(w, r)
			return
		}
	}

	// All other request paths except / are discarded
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Get hostname in request
	hostname := r.Host

	// Try to serve image from cache
	served := false
	// Get random image from local folder
	files, err := ioutil.ReadDir(config.CacheFolder)
	if err != nil {
		log.Println("Error:", err)
	} else {
		if len(files) == 0 {
			log.Println("Error:", "No image found in cache folder")
		} else {
			rand.Seed(time.Now().UnixNano())
			fileIndex := rand.Intn(len(files))
			// Make sure the file is an image
			for !isImage(files[fileIndex].Name()) && len(files) > 0 {
				// If the file is a directory, remove it from the list and get a new random file
				if files[fileIndex].IsDir() {
					files = append(files[:fileIndex], files[fileIndex+1:]...)
					if len(files) == 0 {
						break
					}
					fileIndex = rand.Intn(len(files))
					continue
				}
				// Remove the non-image file
				err = os.Remove(config.CacheFolder + string(os.PathSeparator) + files[fileIndex].Name())
				if err != nil {
					log.Println("Error:", err)
				}
				files = append(files[:fileIndex], files[fileIndex+1:]...)
				if len(files) == 0 {
					break
				}
				fileIndex = rand.Intn(len(files))
			}
			// If the file is still not an image, log error and retrieve from remote later
			if len(files) == 0 || !isImage(files[fileIndex].Name()) {
				log.Println("Error:", "No image found in cache folder")
			} else {
				// Serve image
				log.Println("Serving local image: ", files[fileIndex].Name())
				fmt.Fprintf(w, "http://%s/%s/%s", hostname, config.CacheFolder, files[fileIndex].Name())
				served = true
			}
		}
	}

	// Determine whether to access remote to retrieve more images
	if served && (config.Mode == Local || time.Now().Unix()-timestamp < config.UpdateInterval) {
		return
	} else {
		if served {
			// If we've served an image from local, but it's time to update, update in background
			go func() {
				retrieveRemote(hostname, w, r)
			}()
		} else {
			// If we didn't serve image from local, retrieve from remote
			retrieveRemote(hostname, w, r)
		}
	}
}

func main() {
	// Create/Read config file
	config = getConfig()
	// Initialize logging
	var logOutput io.Writer
	if config.LogFileName != "" {
		logFile, err := os.OpenFile(config.LogFileName, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			log.Fatalln("Error:", err)
		}
		defer logFile.Close()
		logOutput = io.MultiWriter(os.Stdout, logFile)
	} else {
		logOutput = os.Stdout
	}
	log.SetOutput(logOutput)
	log.Println("Initialized Config: \n", getConfigString(config))

	// Initialize last update timestamp
	timestamp = time.Now().Unix()

	// Start server
	http.HandleFunc("/", handleRequest)
	http.HandleFunc("/reload", reloadConfig)
	log.Println("Listening on port: ", config.ListenPort)
	log.Fatalln(http.ListenAndServe(":"+strconv.Itoa(config.ListenPort), nil))
}
