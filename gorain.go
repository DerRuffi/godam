package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//var jobs = make(chan Job, 10)
//var results = make(chan Result, 10)
var mcombstress map[uint32]float64

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

type Cycle struct {
	high  float64
	low   float64
	count float64
	seq   float64
}
type Job struct {
	id int
	fl Flight
	//ser []float64
}
type Result struct {
	job Job
	seq float64
}
type Loadcases struct {
	Loadcases []CombLC `json:"loadcases"`
}
type CombLC struct {
	Lcid   uint32  `json:"lcid"`
	Unilcs []UniLC `json:"unilcs"`
}
type UniLC struct {
	Fueid  int     `json:"fueid"`
	Factor float64 `json:"factor"`
}
type Flights struct {
	Flights []Flight `json:"flights"`
	//	mcombstress map[uint32]float64
}
type Flight struct {
	FlName string   `json:"flightname"`
	Occ    int      `json:"occurance"`
	LCSeq  []uint32 `json:"sequence"`
}

// Start of filght def file reading

func readloadcase(filepath string) Loadcases {
	// Open our jsonFile
	jsonFile, err := os.Open(filepath)
	// if we os.Open returns an error then handle it
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("Successfully Opened loadcase file.")
	// defer the closing of our jsonFile so that we can parse it later on
	defer jsonFile.Close()

	// read our opened xmlFile as a byte array.
	byteValue, _ := ioutil.ReadAll(jsonFile)

	// we initialize our Users array
	var lcs Loadcases

	// we unmarshal our byteArray which contains our
	// jsonFile's content into 'users' which we defined above
	json.Unmarshal(byteValue, &lcs)
	return lcs
}

func readflights(filepath string) Flights {
	jsonFile, err := os.Open(filepath)

	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Successfully Opened flightdef file.")
	defer jsonFile.Close()

	byteValue, _ := ioutil.ReadAll(jsonFile)

	var fls Flights
	json.Unmarshal(byteValue, &fls)
	/* 	for _, fl := range fls.Flights {
		fmt.Println(fl.LCSeq)
	} */
	return fls
}

func readstf1d(filepath string) map[int]float64 {
	//var mstf map[int]float64
	mstf := make(map[int]float64)
	csvfile, err := os.Open(filepath)
	// if we os.Open returns an error then handle it
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Successfully Opened Stf file", filepath)
	// defer the closing of our jsonFile so that we can parse it later on
	defer csvfile.Close()
	r := csv.NewReader(csvfile)
	r.Comma = '\t'
	r.Comment = '#'
	i := 0
	for {
		// iterate the lines
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if i > 0 {
			fue, err := strconv.Atoi(strings.ReplaceAll(record[0], " ", ""))
			if err != nil {
				fmt.Println(err)
			}
			stress, err2 := strconv.ParseFloat(strings.ReplaceAll(record[1], " ", ""), 64)
			if err2 != nil {
				fmt.Println(err2)
			}
			mstf[fue] = stress
		}
		i += 1
	}
	//fmt.Println(mstf)
	return mstf
}

// Start of sequence calcs
func combinefue(mstf map[int]float64, lcs Loadcases) map[uint32]float64 {
	mcombstress := make(map[uint32]float64)
	var sums, fuestress float64
	for _, combl := range lcs.Loadcases {
		sums = 0

		unilcs := combl.Unilcs
		for _, unilc := range unilcs {
			fuestress = mstf[unilc.Fueid]
			sums += unilc.Factor * fuestress
		}
		mcombstress[combl.Lcid] = sums
	}
	//fmt.Println(mcombstress)
	return mcombstress
}

func generatesequence(fl Flight, mcombstress map[uint32]float64) chan float64 {
	c := make(chan float64, 1000)
	go func() {
		for _, comblc := range fl.LCSeq {
			c <- mcombstress[comblc]
		}
		close(c)
	}()
	return c
}

// Set Method for Cycle struct
func (myc *Cycle) SetCycle(p1, p2, count float64) {
	myc.count = count
	if p1 < p2 {
		myc.low = p1
		myc.high = p2
	} else {
		myc.low = p2
		myc.high = p1
	}
	rratio := myc.low / myc.high
	myc.seq = math.Pow(((1-rratio)/0.9), 0.6) * myc.high
	//fmt.Printf("Max%f,Min%f,Seq%f\n", myc.high,myc.low,myc.seq)
}

func rev(s []float64, omit float64) chan float64 {
	c := make(chan float64, 1000)
	go func() {
		var x_last, d_next, d_last, x float64
		x_last, x = s[0], s[1]
		d_last = x - x_last
		c <- x_last
		for _, x_next := range s[2:] {
			if x_next == x || math.Abs(x_next-x) < omit {
				continue
			}
			d_next = x_next - x
			if d_last*d_next < 0 {
				c <- x
			}
			x_last = x
			x = x_next
			d_last = d_next
		}
		c <- x
		close(c)
	}()
	return c
}

func revchan(s <-chan float64, omit float64) chan float64 {
	c := make(chan float64, 1000)
	go func() {
		var x_last, d_next, d_last, x float64
		i := 0
		for x_next := range s {
			if i == 0 {
				x_last = x_next
				i += 1
				continue
			}
			if i == 1 {
				x = x_next
				d_last = x - x_last
				c <- x_last
				i += 1
				continue
			}
			if x_next == x || math.Abs(x_next-x) < omit {
				i += 1
				continue
			}
			d_next = x_next - x
			if d_last*d_next < 0 {
				c <- x
			}
			x_last = x
			x = x_next
			d_last = d_next
			i += 1
		}
		c <- x
		close(c)
	}()
	return c
}

func extr(chrev <-chan float64) chan Cycle {
	c := make(chan Cycle, 100)
	go func() {
		var ps []float64 = nil
		var lenps int
		for point := range chrev {
			ps = append(ps, point)
			lenps = len(ps)
			for lenps >= 3 {
				x1, x2, x3 := ps[lenps-3], ps[lenps-2], ps[lenps-1]
				X := math.Abs(float64(x3 - x2))
				Y := math.Abs(float64(x2 - x1))
				if X < Y {
					break
				} else if lenps == 3 {
					var mycycle = Cycle{}
					mycycle.SetCycle(ps[0], ps[1], 0.5)
					c <- mycycle
					ps = ps[1:]
				} else {
					var mycycle = Cycle{}
					mycycle.SetCycle(ps[lenps-3], ps[lenps-2], 1.0)
					c <- mycycle
					ps[lenps-3] = ps[lenps-1]
					ps = ps[:lenps-2]
				}
				lenps = len(ps)
			}
		}
		lenps = len(ps)
		//fmt.Println("Last cycle, lenps: ", lenps)
		for lenps > 1 {
			var mycycle = Cycle{}
			mycycle.SetCycle(ps[0], ps[1], 0.5)
			c <- mycycle
			ps = ps[1:]
			lenps = len(ps)

		}
		close(c)
	}()
	return c
}

func seqCalc(chcyc <-chan Cycle, pval, omlevel float64) float64 {
	var sumseq float64 = 0
	for cycle := range chcyc {
		if cycle.seq >= omlevel {
			sumseq += math.Pow(cycle.seq, pval) * cycle.count
			//fmt.Printf("Seq%f\n", cycle.seq)
		}
	}
	sumseq2 := math.Pow(sumseq, (1 / pval))
	//fmt.Printf("Seqsum%f\n", sumseq2)
	return sumseq2
}

/* func randFloats(min, max float64, n int) []float64 {
	res := make([]float64, n)
	for i := range res {
		res[i] = min + rand.Float64()*(max-min)
	}
	return res
}

func makeFlights(nofl, nop int) {
	for i := 0; i < nofl; i++ {
		ser := randFloats(30.0, 75.0, nop)
		job := Job{i, ser}
		jobs <- job
	}
	close(jobs)
} */

// Start worker pools
func worker(wg *sync.WaitGroup, results chan<- Result, jobs <-chan Job) {
	for job := range jobs {
		//output := Result{job, digits(job.randomno)}
		sequence := generatesequence(job.fl, mcombstress)
		chrev := revchan(sequence, 0.0)
		chcyc := extr(chrev)
		output := Result{job, seqCalc(chcyc, 4.6, 11.0)}
		results <- output
	}
	wg.Done()
}
func createWorkerPool(noOfWorkers int, results chan Result, jobs chan Job) {
	var wg sync.WaitGroup
	for i := 0; i < noOfWorkers; i++ {
		wg.Add(1)
		go worker(&wg, results, jobs)
	}
	wg.Wait()
	close(results)
}
func result(done chan bool, wg2 *sync.WaitGroup, results <-chan Result) {
	for result := range results {
		fmt.Printf("Job id %d,Seq%f\n", result.job.id, result.seq)
		//continue
	}
	wg2.Done()
	done <- true
}

func genrateflights(fls Flights, jobs chan<- Job) {
	for i, fl := range fls.Flights {
		//ser := randFloats(30.0, 75.0, nop)
		job := Job{i, fl}
		jobs <- job
	}
	close(jobs)
}

// End worker pools

func getstffiles(stfdir string) []string {
	var stf1dfiles []string
	files, err := ioutil.ReadDir(stfdir)
	if err != nil {
		fmt.Println("Failed reading folders")
		log.Fatal(err)
	}
	for _, file := range files {
		fileExtension := filepath.Ext(file.Name())
		if fileExtension == ".stf" {
			stf1dfiles = append(stf1dfiles, file.Name())
		}
	}
	return stf1dfiles
}

func main() {
	var wg2 sync.WaitGroup
	start := time.Now()
	loadcasefile := "loadcases.json"
	lcs := readloadcase(loadcasefile)
	flightsfile := "flightdef.json"
	fls := readflights(flightsfile)
	duration := time.Since(start)
	fmt.Println("Flight def loaded in: ", duration)
	// Start stf files
	startcalc := time.Now()
	stfdir := "./1d/"
	stffiles := getstffiles(stfdir)
	for _, stffile := range stffiles {
		var jobs = make(chan Job, 10)
		var results = make(chan Result, 10)
		stfpath := stfdir + stffile
		mstf := readstf1d(stfpath)
		mcombstress = combinefue(mstf, lcs)
		//wg.Add(1)
		go genrateflights(fls, jobs)
		done := make(chan bool)
		wg2.Add(1)
		go result(done, &wg2, results)
		noOfWorkers := 2
		createWorkerPool(noOfWorkers, results, jobs)
		wg2.Wait()
		<-done
	}
	duration = time.Since(startcalc)
	fmt.Println(duration)
	fmt.Println("Done")
}

/* func mainold() {
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	//ser := []float64{0,50,40,90,80,90,30,10}
	//mfl := makeFlights(100,100000)
	rand.Seed(time.Now().UnixNano())
	//ser := randFloats(30.0, 35.0, 10000000)
	start := time.Now()

	   chrev := rev(ser, 2.0)
	   chcy := extr(chrev)
	   for range chcy {
	       //fmt.Println("End",i)
	       continue
	   }


	   for _, ser := range mfl {
	       chrev := rev(ser, 2.0)
	       chcy := extr(chrev)
	       for range chcy {
	           //fmt.Println("End",i)
	           continue
	       }
	   }

	nofl := 10
	nop := 100
	go makeFlights(nofl, nop)
	done := make(chan bool)
	go result(done)
	noOfWorkers := 10
	createWorkerPool(noOfWorkers)
	<-done
	duration := time.Since(start)
	fmt.Println(duration)
	fmt.Println("Done")
}
*/
