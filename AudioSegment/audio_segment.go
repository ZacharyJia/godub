package AudioSegment

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/cryptix/wav"
	"io/ioutil"
	"math"
	"os"
)

type WavSubChunk struct {
	id       []byte
	position uint32
	size     uint32
}

type WavData struct {
	audio_format    uint16
	channels        uint16
	sample_rate     uint32
	bits_per_sample uint16
	raw_data        []byte
}

type AudioSegment struct {
	data         *[]byte
	channels     uint16
	frame_rate   uint32
	frame_width  uint16
	sample_width uint16
}

func (p *AudioSegment) FrameCount() int {
	return len(*p.data) / int(p.frame_width)
}

func (p *AudioSegment) FrameCountMs(ms int) int {
	return ms * (int(p.frame_rate) / 1000.0)
}

func (p *AudioSegment) Len() int {
	return int(math.Round(1000 * float64(p.FrameCount()) / float64(p.frame_rate)))
}

func (p *AudioSegment) Slice(start, end int) *AudioSegment {
	data := (*p.data)[start:end]
	return p.spawn(&data)
}

func (p *AudioSegment) parsePosition(val int) int {
	if val < 0 {
		val = p.Len() + val
	}

	return p.FrameCountMs(val)
}

func (p *AudioSegment) Overlay(seg *AudioSegment) *AudioSegment {
	return p
}

func (p *AudioSegment) Fade() *AudioSegment {
	return p
}



func (p *AudioSegment) AppendCrossfage(seg *AudioSegment, crossfade int) *AudioSegment {
	//TODO: need to sync two audiosegment
	seg1 := p
	seg2 := seg

	if crossfade == 0 {
		data := append(*p.data, *seg.data...)
		return p.spawn(&data)
	} else if crossfade > p.Len() {
		errmsg := fmt.Sprintf("Crossfade is longer than the original AudioSegment (%dms > %dms)", crossfade, p.Len())
		panic(errmsg)
	} else if crossfade > seg.Len() {
		errmsg := fmt.Sprintf("Crossfade is longer than the appended AudioSegment (%dms > %dms)", crossfade, seg.Len())
		panic(errmsg)
	}

	xf := p.Slice(-crossfade, p.Len()).Fade()
	xf.Overlay(seg.Slice(0, crossfade).Fade())

}

func (p *AudioSegment) Append(seg *AudioSegment) *AudioSegment {
	return p.AppendCrossfage(seg, 0)
}

func (p *AudioSegment) saveWav(file *os.File) {
	meta := wav.File{
		Channels:        p.channels,
		SampleRate:      p.frame_rate,
		SignificantBits: p.sample_width * 8,
	}
	writer, err := meta.NewWriter(file)
	if err != nil {
		panic(err)
	}
	defer writer.Close()
	writer.Write(*p.data)
}

func (p *AudioSegment) spawn(data *[]byte) *AudioSegment {
	as := *p
	as.data = data
	return &as
}

func (p *AudioSegment) Export(out_f string, format string) {
	if format == "wav" {
		fd, err := os.Create(out_f)
		if err != nil {
			panic(err)
		}
		p.saveWav(fd)
	}
}

func bytes2UInt(b []byte, order binary.ByteOrder) uint32 {
	bytesBuffer := bytes.NewBuffer(b)
	var tmp uint32
	binary.Read(bytesBuffer, order, &tmp)
	return tmp
}

func bytes2UShort(b []byte, order binary.ByteOrder) uint16 {
	bytesBuffer := bytes.NewBuffer(b)
	var tmp uint16
	binary.Read(bytesBuffer, order, &tmp)
	return tmp
}

func fd_or_tempfile(file string, tempfile bool) (pFile *os.File, err error) {
	if file == "" && tempfile == true {
		pFile, err = ioutil.TempFile("", "godub")
		return
	} else {
		pFile, err = os.Open(file)
		return
	}
}

func extract_wav_headers(data *[]byte) []WavSubChunk {
	var pos uint32 = 12
	subchunks := make([]WavSubChunk, 0, 2)
	for pos+8 < uint32(len(*data)) && len(subchunks) < 10 {
		subchunk_id := (*data)[pos : pos+4]
		subchunk_size := bytes2UInt((*data)[pos+4:pos+8], binary.LittleEndian)
		subchunks = append(subchunks, WavSubChunk{id: subchunk_id, position: pos, size: subchunk_size})
		if bytes.Equal(subchunk_id, []byte{'d', 'a', 't', 'a'}) {
			break
		}
		pos += subchunk_size + uint32(8)
	}
	return subchunks
}

func read_wav_data(data *[]byte) WavData {
	headers := extract_wav_headers(data)
	fmts := make([]WavSubChunk, 0, 2)
	for i := 0; i < len(headers); i++ {
		if bytes.Equal(headers[i].id, []byte{'f', 'm', 't', ' '}) {
			fmts = append(fmts, headers[i])
		}
	}
	if len(fmts) == 0 || fmts[0].size < 16 {
		panic("Couldn't find fmts header in wav data")
	}
	format := fmts[0]
	pos := format.position + 8
	audio_format := bytes2UShort((*data)[pos:pos+2], binary.LittleEndian)
	if audio_format != 1 && audio_format != 0xFFFE {
		errmsg := fmt.Sprintf("Unknown audio format 0x%X in wav data", audio_format)
		panic(errmsg)
	}
	channels := bytes2UShort((*data)[pos+2:pos+4], binary.LittleEndian)
	sample_rate := bytes2UInt((*data)[pos+4:pos+8], binary.LittleEndian)
	bits_per_sample := bytes2UShort((*data)[pos+14:pos+16], binary.LittleEndian)

	data_hdr := headers[len(headers)-1]
	if !bytes.Equal(data_hdr.id, []byte{'d', 'a', 't', 'a'}) {
		panic("Couldn't find data header in wav data")
	}
	pos = data_hdr.position + 8
	return WavData{
		audio_format:    audio_format,
		channels:        channels,
		sample_rate:     sample_rate,
		bits_per_sample: bits_per_sample,
		raw_data:        (*data)[pos : pos+data_hdr.size]}
}

func from_safe_wav(file string) *AudioSegment {
	f, err := fd_or_tempfile(file, false)
	if err != nil {
		panic(err)
	}
	f.Seek(0, 0)
	obj := new_audio_segment_with_wav(f)
	f.Close()
	return obj
}

func From_file(file string, format string) *AudioSegment {
	if format == "wav" {
		return from_safe_wav(file)
	}
	return nil
}

func new_audio_segment_with_wav(file *os.File) *AudioSegment {
	data, err := ioutil.ReadAll(file)
	if err != nil {
		panic(err)
	}
	obj := AudioSegment{}
	wav_data := read_wav_data(&data)
	obj.channels = wav_data.channels
	obj.sample_width = wav_data.bits_per_sample / 8
	obj.frame_rate = wav_data.sample_rate
	obj.frame_width = obj.channels * obj.sample_width
	obj.data = &wav_data.raw_data

	if obj.sample_width == 3 {
		// needs to be converted from 21-bit to 32-bit
		panic("sample cannot be 24-bit")
	}

	return &obj
}

func NewAudioSegment() *AudioSegment {
	return &AudioSegment{}
}
