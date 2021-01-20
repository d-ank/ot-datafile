package datafile

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffail/gabs"
	"golang.org/x/sys/windows/registry"
)

// Hook is a hooked onetap datafile
type Hook struct {
	Name   string
	Reader chan json.RawMessage
	close  chan struct{}
}

// Parse outputs a json string from the onetap datafile format, so long as the data values are also a json
func Parse(str []byte) (json.RawMessage, error) {
	r := regexp.MustCompile(`[{\[]{1}([,:{}\[\]0-9.\-+Eaeflnr-u \n\r\t]|".*?")+[}\]]{1}`)
	b, err := hex.DecodeString(strings.Replace(string(str), "\n", "", -1))
	if err != nil {
		return nil, err
	}
	matches := r.FindAllString(string(b), -1)
	if len(matches) == 0 {
		return nil, errors.New("failed to find json")
	}
	en := make(map[int]string)
	for _, v := range matches {
		c, err := gabs.ParseJSON([]byte(v))
		if err != nil {
			continue
		}
		//discard the meta object
		if c.Exists("meta") {
			continue
		}
		id, ok := c.Path("id").Data().(string)
		if !ok {
			continue
		}
		val := c.Path("v").Data().(string)
		index, err := strconv.Atoi(strings.Split(id, "-")[1])
		if err != nil {
			continue
		}
		en[index] = val
	}
	var arr []string
	for _, str := range en {
		arr = append(arr, str)
	}
	dec, err := base64.RawStdEncoding.DecodeString(strings.Join(arr[:], ""))
	if err != nil {
		return nil, err
	}
	return dec, nil
}

// Add takes in a datafile in the ot/scripts subfolder and returns a Hook
func Add(datafile string) (Hook, error) {
	query, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Steam App 730`, registry.QUERY_VALUE)
	if err != nil {
		return Hook{}, err
	}
	defer query.Close()
	loc, _, err := query.GetStringValue("InstallLocation")
	if err != nil {
		return Hook{}, err
	}
	hook := Hook{
		Name:   datafile,
		Reader: make(chan json.RawMessage),
		close:  make(chan struct{}),
	}
	// pass back to the reader channel and writes to the datafile
	go func(hook Hook) {
		lastStat, err := os.Stat(loc + `\ot\scripts\` + datafile)
		if err != nil {
			<-hook.close
			return
		}
		for {
			select {
			default:
				stat, err := os.Stat(loc + `\ot\scripts\` + datafile)
				if err != nil {
					time.Sleep(time.Millisecond * 2000)
					continue
				}
				if stat.Size() == lastStat.Size() || stat.ModTime() == lastStat.ModTime() {
					time.Sleep(time.Millisecond * 3)
					continue
				} else {
					lastStat = stat
				}
				data, err := ioutil.ReadFile(loc + `\ot\scripts\` + datafile)
				if err != nil {
					continue
				}
				json, err := Parse(data)
				if err != nil {
					continue
				}
				hook.Reader <- json
			case <-hook.close:
				return
			}
		}
	}(hook)
	return hook, nil
}

// Close the hook on the datafile
func (hook Hook) Close() {
	<-hook.close
}
