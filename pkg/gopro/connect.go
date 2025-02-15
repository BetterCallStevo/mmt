package gopro

/* GoPro Connect - API exposed over USB Ethernet */

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/konradit/mmt/pkg/utils"
)

var ipAddress = ""

func handleKill() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		color.Red("\nKilling program, exiting Turbo mode.")
		caller(ipAddress, "gp/gpTurbo?p=0", nil)
		os.Exit(0)
	}()
}
func caller(ip, path string, object interface{}) error {

	client := &http.Client{}
	req, err := http.NewRequest("GET", fmt.Sprintf("http://%s/%s", ip, path), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if object != nil {
		err = json.NewDecoder(resp.Body).Decode(object)
		if err != nil {
			return err
		}
	}
	return nil
}

func GetGoProNetworkAddresses() ([]GoProConnectDevice, error) {
	ipsFound := []GoProConnectDevice{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ipsFound, err
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			r := regexp.MustCompile(`172.2\d.\d\d\d.5\d`)
			ipv4Addr := a.(*net.IPNet).IP.To4()
			if r.MatchString(ipv4Addr.String()) {
				correctIP := ipv4Addr.String()[:len(ipv4Addr.String())-1] + "1"
				var gpInfo = &cameraInfo{}
				err := caller(correctIP, "gp/gpControl/info", gpInfo)
				if err != nil {
					continue
				}
				ipsFound = append(ipsFound, GoProConnectDevice{
					IP:   correctIP,
					Info: *gpInfo,
				})
			}
		}
	}
	return ipsFound, nil
}

func getThumbnailFilename(filename string) string {
	replacer := strings.NewReplacer("H", "L", "X", "L", "MP4", "LRV")
	return replacer.Replace(filename)
}
func ImportConnect(in, out string, sortOptions SortOptions) (*utils.Result, error) {
	var result utils.Result

	ipAddress = in
	handleKill()
	// activate turbo
	err := caller(in, "gp/gpTurbo?p=1", nil)
	if err != nil {
		return nil, err
	}

	var gpMediaList = &goProMediaList{}
	err = caller(in, "gp/gpMediaList", gpMediaList)
	if err != nil {
		return nil, err
	}
	var gpInfo = &cameraInfo{}
	err = caller(in, "gp/gpControl/info", gpInfo)
	if err != nil {
		return nil, err
	}
	cameraName := gpInfo.Info.ModelName
	for _, folder := range gpMediaList.Media {
		for _, goprofile := range folder.Fs {

			for _, fileTypeMatch := range FileTypeMatches[V2] {

				if fileTypeMatch.Regex.MatchString(goprofile.N) {

					i, err := strconv.ParseInt(goprofile.Mod, 10, 64)
					if err != nil {
						continue
					}
					tm := time.Unix(i, 0)
					mediaDate := tm.Format("02-01-2006")

					if strings.Contains(sortOptions.DateFormat, "year") && strings.Contains(sortOptions.DateFormat, "month") && strings.Contains(sortOptions.DateFormat, "day") {
						mediaDate = tm.Format(replacer.Replace(sortOptions.DateFormat))
					}

					start := sortOptions.DateRange[0]
					end := sortOptions.DateRange[1]
					if tm.Before(start) {
						continue
					}
					if tm.After(end) {
						continue
					}

					dayFolder := filepath.Join(out, mediaDate)
					if _, err := os.Stat(dayFolder); os.IsNotExist(err) {
						os.Mkdir(dayFolder, 0755)
					}

					if sortOptions.ByCamera {
						if _, err := os.Stat(filepath.Join(dayFolder, cameraName)); os.IsNotExist(err) {
							os.Mkdir(filepath.Join(dayFolder, cameraName), 0755)
						}
						dayFolder = filepath.Join(dayFolder, cameraName)
					}

					switch fileTypeMatch.Type {
					case Video:
						x := goprofile.N
						filename := fmt.Sprintf("%s%s-%s.%s", x[:2], x[4:][:4], x[2:][:2], strings.Split(x, ".")[1])
						color.Green(">>> %s", x)

						if _, err := os.Stat(filepath.Join(dayFolder, "videos")); os.IsNotExist(err) {
							err = os.MkdirAll(filepath.Join(dayFolder, "videos"), 0755)
							if err != nil {
								log.Fatal(err.Error())
							}
						}

						err := utils.DownloadFile(filepath.Join(dayFolder, "videos", filename), fmt.Sprintf("http://%s:8080/videos/DCIM/%s/%s", in, folder.D, goprofile.N))
						if err != nil {
							result.Errors = append(result.Errors, err)
							result.FilesNotImported = append(result.FilesNotImported, goprofile.N)
						} else {
							result.FilesImported += 1
						}
						if !sortOptions.SkipAuxiliaryFiles {
							if _, err := os.Stat(filepath.Join(dayFolder, "videos/proxy")); os.IsNotExist(err) {
								err = os.MkdirAll(filepath.Join(dayFolder, "videos/proxy"), 0755)
								if err != nil {
									log.Fatal(err.Error())
								}
							}

							x := goprofile.N
							filename := fmt.Sprintf("%s%s-%s.%s", x[:2], x[4:][:4], x[2:][:2], strings.Split(x, ".")[1])
							utils.DownloadFile(filepath.Join(dayFolder, "videos/proxy", filename), fmt.Sprintf("http://%s:8080/videos/DCIM/%s/%s", in, folder.D, getThumbnailFilename(goprofile.N)))
						}
					case Photo:
						if _, err := os.Stat(filepath.Join(dayFolder, "photos")); os.IsNotExist(err) {
							err = os.MkdirAll(filepath.Join(dayFolder, "photos"), 0755)
							if err != nil {
								log.Fatal(err.Error())
							}
						}

						color.Green(">>> %s", goprofile.N)

						err := utils.DownloadFile(filepath.Join(dayFolder, "photos", goprofile.N), fmt.Sprintf("http://%s:8080/videos/DCIM/%s/%s", in, folder.D, goprofile.N))
						if err != nil {
							result.Errors = append(result.Errors, err)
							result.FilesNotImported = append(result.FilesNotImported, goprofile.N)
						} else {
							result.FilesImported += 1
						}
					case Multishot:
						filebaseroot := goprofile.N[:4]
						if _, err := os.Stat(filepath.Join(dayFolder, "multishot", filebaseroot)); os.IsNotExist(err) {
							err = os.MkdirAll(filepath.Join(dayFolder, "multishot", filebaseroot), 0755)
							if err != nil {
								log.Fatal(err.Error())
							}
						}

						color.Green(">>> %s/%s", filebaseroot, goprofile.N)

						err := utils.DownloadFile(filepath.Join(dayFolder, "multishot", goprofile.N), fmt.Sprintf("http://%s:8080/videos/DCIM/%s/%s", in, folder.D, goprofile.N))
						if err != nil {
							result.Errors = append(result.Errors, err)
							result.FilesNotImported = append(result.FilesNotImported, goprofile.N)
						} else {
							result.FilesImported += 1
						}

					case RawPhoto:
						if _, err := os.Stat(filepath.Join(dayFolder, "photos/raw")); os.IsNotExist(err) {
							err = os.MkdirAll(filepath.Join(dayFolder, "photos/raw"), 0755)
							if err != nil {
								log.Fatal(err.Error())
							}
						}

						color.Green(">>> %s", goprofile.N)
						// convert to DNG here
						err := utils.DownloadFile(filepath.Join(dayFolder, "photos/raw", goprofile.N), fmt.Sprintf("http://%s:8080/videos/DCIM/%s/%s", in, folder.D, goprofile.N))
						if err != nil {
							result.Errors = append(result.Errors, err)
							result.FilesNotImported = append(result.FilesNotImported, goprofile.N)
						} else {
							result.FilesImported += 1
						}
					default:
						color.Red("Unsupported file %s", goprofile.N)
						result.Errors = append(result.Errors, errors.New("Media format unrecognized"))
						result.FilesNotImported = append(result.FilesNotImported, goprofile.N)
					}
				}
			}
		}
	}
	caller(ipAddress, "gp/gpTurbo?p=0", nil)
	return &result, nil
}
