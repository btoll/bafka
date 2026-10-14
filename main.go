package main

import (
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	linkscanner "github.com/btoll/link-scanner"
)

var (
	output     string
	sitemap    string
	uri        string
	numWorkers int
)

type URLSet struct {
	Links []struct {
		Location string `xml:"loc"`
		LastMod  string `xml:"lastmod"`
	} `xml:"url"`
}

type ScannedLinks struct {
	Results *linkscanner.ScanResults
	Err     error
}

type Status struct {
	StatusCode int
	URL        string
}

func filterSitemapLinks(ctx context.Context, in <-chan Status) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		for status := range in {
			select {
			case <-ctx.Done():
				return
			default:
				if http.StatusOK == status.StatusCode {
					select {
					case out <- status.URL:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return out
}

// TODO: Handle this error!
func getAbsoluteURI(link string) string {
	absoluteUrl, _ := url.Parse(uri)
	l, _ := absoluteUrl.Parse(link)
	return l.String()
}

// TODO: Handle these errors!
func getHead(link string) Status {
	req, err := http.NewRequest(http.MethodHead, getAbsoluteURI(link), nil)
	if err != nil {
		fmt.Println(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println(err)
	}
	return Status{
		StatusCode: resp.StatusCode,
		URL:        link,
	}
}

func getSitemapLinks(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create sitemap request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch sitemap: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read sitemap response: %w", err)
	}

	urlset := URLSet{}
	err = xml.Unmarshal(b, &urlset)
	if err != nil {
		return nil, fmt.Errorf("parse sitemap xml: %w", err)
	}
	links := make([]string, 0, len(urlset.Links))
	for _, link := range urlset.Links {
		if link.Location != "" && link.LastMod != "" {
			links = append(links, link.Location)
		}
	}
	return links, nil
}

func scanLinks(ctx context.Context, in <-chan string) <-chan ScannedLinks {
	var wg sync.WaitGroup

	out := make(chan ScannedLinks)
	jobs := make(chan string)

	for range numWorkers {
		wg.Go(func() {
			for link := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
					scanResults, err := linkscanner.ProcessURL(ctx, getAbsoluteURI(link))
					out <- ScannedLinks{
						Results: scanResults,
						Err:     err,
					}
				}
			}
		})
	}

	go func() {
		defer close(jobs)
		for link := range in {
			select {
			case jobs <- link:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func testSitemapLinks(ctx context.Context, links []string) <-chan Status {
	var wg sync.WaitGroup

	out := make(chan Status, len(links))
	jobs := make(chan string)

	for range numWorkers {
		wg.Go(func() {
			for link := range jobs {
				select {
				case out <- getHead(link):
				case <-ctx.Done():
					return
				}
			}
		})
	}

	go func() {
		defer close(jobs)
		for _, link := range links {
			select {
			case jobs <- link:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}

func pipeline(ctx context.Context, links []string) <-chan ScannedLinks {
	return scanLinks(ctx,
		filterSitemapLinks(ctx,
			testSitemapLinks(ctx, links)))
}

func main() {
	flag.StringVar(&uri, "uri", "", "The target site.")
	flag.StringVar(&sitemap, "sitemap", "sitemap.xml", "The list of available links.")
	flag.IntVar(&numWorkers, "workers", 20, "The number of workers in the worker pool.")
	flag.StringVar(&output, "output", "", "Instead of writing to stdout, write to a file.")
	flag.Parse()

	if uri == "" {
		log.Fatalln("[ERROR] You must privide a url.")
	}

	sitemapURL := fmt.Sprintf("%s/%s", uri, sitemap)
	var f *os.File
	var buf *bufio.Writer
	if output != "" {
		root, err := os.OpenRoot("/tmp")
		if err != nil {
			fmt.Println(err)
		}
		f, err = root.Create(filepath.Base(output))
		if err != nil {
			fmt.Println(err)
		}
		buf = bufio.NewWriter(f)
		defer f.Close()
	}

	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	s := make(chan os.Signal, 1)
	signal.Notify(s, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(s)

	go func() {
		sig := <-s
		cancel(fmt.Errorf("Received signal %s", sig))
		fmt.Printf("Received signal %s, canceling...\n", sig)
	}()

	links, err := getSitemapLinks(ctx, sitemapURL)
	if err != nil {
		log.Fatalf("fetch sitemap links: %v", err)
	}

	for scannedLinks := range pipeline(ctx, links) {
		if scannedLinks.Err != nil {
			log.Fatalf("scanned links: %v\n", err)
		}
		failures := scannedLinks.Results.GetFailures()
		if len(failures.LinkResults) > 0 {
			b, err := json.Marshal(failures)
			if err != nil {
				fmt.Println(err)
			}
			if f == nil {
				fmt.Println(string(b))
				continue
			}
			if _, err := buf.Write(b); err != nil {
				log.Fatalf("write buffer: %v\n", err)
			}
			if err := buf.WriteByte('\n'); err != nil {
				log.Fatalf("write byte: %v\n", err)
			}
		}
	}

	if f != nil {
		if context.Cause(ctx) != nil {
			fmt.Printf("Deleting file %s\n", f.Name())
			if err := f.Close(); err != nil {
				log.Fatalf("close file: %v\n", err)
			}
			if err := os.Remove(f.Name()); err != nil {
				log.Fatalf("remove file: %v\n", err)
			}
		} else {
			if err := buf.Flush(); err != nil {
				log.Fatalf("flush buffer: %v\n", err)
			}
			if err := f.Close(); err != nil {
				log.Fatalf("close file: %v\n", err)
			}
		}
	}
}
