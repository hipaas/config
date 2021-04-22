package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hipaas/config/source"
	"github.com/hipaas/config/source/env"
	"github.com/hipaas/config/source/etcd"
	"github.com/hipaas/config/source/file"
	"github.com/hipaas/config/source/memory"
)

func createFileForIssue18(t *testing.T, content string) *os.File {
	data := []byte(content)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("file.%d", time.Now().UnixNano()))
	fh, err := os.Create(path)
	if err != nil {
		t.Error(err)
	}
	_, err = fh.Write(data)
	if err != nil {
		t.Error(err)
	}

	return fh
}

func createFileForTest(t *testing.T) *os.File {
	data := []byte(`{"foo": "bar"}`)
	path := filepath.Join(os.TempDir(), fmt.Sprintf("file.%d", time.Now().UnixNano()))
	fh, err := os.Create(path)
	if err != nil {
		t.Error(err)
	}
	_, err = fh.Write(data)
	if err != nil {
		t.Error(err)
	}

	return fh
}

func TestConfigLoadWithGoodFile(t *testing.T) {
	fh := createFileForTest(t)
	path := fh.Name()
	defer func() {
		fh.Close()
		os.Remove(path)
	}()

	// Create new config
	conf, err := NewConfig()
	if err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}
	// Load file source
	if err := conf.Load(file.NewSource(
		file.WithPath(path),
	)); err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}
}

func TestConfigLoadWithInvalidFile(t *testing.T) {
	fh := createFileForTest(t)
	path := fh.Name()
	defer func() {
		fh.Close()
		os.Remove(path)
	}()

	// Create new config
	conf, err := NewConfig()
	if err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}
	// Load file source
	err = conf.Load(file.NewSource(
		file.WithPath(path),
		file.WithPath("/i/do/not/exists.json"),
	))

	if err == nil {
		t.Fatal("Expected error but none !")
	}
	if !strings.Contains(fmt.Sprintf("%v", err), "/i/do/not/exists.json") {
		t.Fatalf("Expected error to contain the unexisting file but got %v", err)
	}
}

func TestConfigMerge(t *testing.T) {
	fh := createFileForIssue18(t, `{
  "amqp": {
    "host": "rabbit.platform",
    "port": 80
  },
  "handler": {
    "exchange": "springCloudBus"
  }
}`)
	path := fh.Name()
	defer func() {
		fh.Close()
		os.Remove(path)
	}()
	os.Setenv("AMQP_HOST", "rabbit.testing.com")

	conf, err := NewConfig()
	if err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}
	if err := conf.Load(
		file.NewSource(
			file.WithPath(path),
		),
		env.NewSource(),
	); err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}

	actualHost := conf.Get("amqp", "host").String("backup")
	if actualHost != "rabbit.testing.com" {
		t.Fatalf("Expected %v but got %v",
			"rabbit.testing.com",
			actualHost)
	}
}

func equalS(t *testing.T, actual, expect string) {
	if actual != expect {
		t.Errorf("Expected %s but got %s", actual, expect)
	}
}

func TestConfigWatcherDirtyOverrite(t *testing.T) {
	n := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(n)

	runtime.GOMAXPROCS(1)

	l := 100

	ss := make([]source.Source, l, l)

	for i := 0; i < l; i++ {
		ss[i] = memory.NewSource(memory.WithJSON([]byte(fmt.Sprintf(`{"key%d": "val%d"}`, i, i))))
	}

	conf, _ := NewConfig()

	for _, s := range ss {
		_ = conf.Load(s)
	}
	runtime.Gosched()

	for i, _ := range ss {
		k := fmt.Sprintf("key%d", i)
		v := fmt.Sprintf("val%d", i)
		equalS(t, conf.Get(k).String(""), v)
	}
}

func TestConfigLoadFromEtcd(t *testing.T) {
	//source := etcd.NewSource(
	//etcd.WithAddress(configSet.Address...),
	//etcd.Auth(configSet.Username, configSet.Password),
	//etcd.WithPrefix(p.configSet.Prefix),
	//etcd.WithPrefix(path),
	//etcd.WithDialTimeout(configSet.Timeout),
	// //optionally strip the provided prefix from the keys, defaults to false
	//etcd.StripPrefix(true))
	//)

	path := "/hipaas/appversion"
	source := etcd.NewSource(
		etcd.WithAddress("127.0.0.1:2379"),
		etcd.WithPrefix(path),
		etcd.WithDialTimeout(time.Second*30),
		etcd.StripPrefix(true),
	)

	conf, err := NewConfig()
	if err != nil {
		t.Fatalf("Expected no error but got %v", err)
	}

	//change, err := source.Read()
	//if err != nil {
	//	t.Fatalf("Expected no error but got %v", err)
	//}
	//
	//fmt.Printf("source.Read:%s\n", change.Data)

	if err = conf.Load(source); err != nil {
		t.Fatalf("Expected no error but got %v\n", err.Error())
	}

	defer conf.Close()

	fmt.Printf("config content:%s\n", conf.Bytes())

	type Version struct {
		V string `json:"v"`
	}
	var onAppVersionChange = func(appVersion interface{}) {
		fmt.Printf("onAppVersionChange got target %s\n", appVersion.(*Version).V)
	}
	var target = &Version{}
	if err = conf.Scan(target); err != nil {
		t.Fatalf("Expected no error but got %v", err.Error())
	} else {
		watch(conf, path, target, onAppVersionChange)
	}
	fmt.Printf("got target %s\n", target.V)

	for {
		fmt.Println("target", target)
		time.Sleep(time.Second * 3)
	}

	fmt.Println("start")
	sleep := make(chan bool, 1)
	<-sleep
	fmt.Println("end")
}

func watch(conf Config, path string, target interface{}, userWatcher func(interface{})) {
	if watcher, err := conf.Watch(); err != nil {
		//panic(err)
	} else {
		go func() {
			for {
				v, err := watcher.Next()
				if err != nil {
					//log.Fatal(err)
					log.Println(err)
				} else {
					//var temp = new(AppConfigData)
					fmt.Printf("v.Bytes:%s\n", v.Bytes())
					if err = json.Unmarshal(v.Bytes(), target); err != nil {
						log.Println(err)
					} else {
						if userWatcher != nil {
							userWatcher(target)
						}
						//fmt.Println(time.Now(), " update to config version ", temp.Version)
						fmt.Println(time.Now(), " update  config ", path)
					}
				}
			}
		}()
	}
}
