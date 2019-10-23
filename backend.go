package erasurecode

/*
#cgo pkg-config: erasurecode-1
#include <stdlib.h>
#include <liberasurecode/erasurecode.h>
#include <liberasurecode/erasurecode_helpers_ext.h>
#include <liberasurecode/erasurecode_postprocessing.h>
// shims to make working with frag arrays easier
char ** makeStrArray(int n) { return calloc(n, sizeof (char *)); }
void freeStrArray(char ** arr) { free(arr); }
void * getStrArrayItem(char ** arr, int idx) { return arr[idx]; }
void setStrArrayItem(char ** arr, int idx, unsigned char * val) { arr[idx] = (char *) val; }
// shims because the fragment headers use misaligned fields
uint64_t getOrigDataSize(struct fragment_header_s *header) { return header->meta.orig_data_size; }
uint32_t getBackendVersion(struct fragment_header_s *header) { return header->meta.backend_version; }
ec_backend_id_t getBackendID(struct fragment_header_s *header) { return header->meta.backend_id; }
uint32_t getECVersion(struct fragment_header_s *header) { return header->libec_version; }

struct encode_chunk_context {
  ec_backend_t instance;
  char **datas;
  char **codings;
  unsigned int number_of_chunks;
  unsigned int chunk_size;
  int blocksize;
  int k;
  int m;
};

void encode_chunk_prepare(int desc,
                          char *data,
                          int datalen,
                          int piecesize,
                          struct encode_chunk_context *ctx)
{
  ctx->instance = liberasurecode_backend_instance_get_by_desc(desc);
  int aligneddatalen = liberasurecode_get_aligned_data_size(desc, datalen);
  int i;
  const int k = ctx->instance->args.uargs.k;
  const int m = ctx->instance->args.uargs.m;

  int blocksize = aligneddatalen / k;

  ctx->blocksize = blocksize;

  ctx->number_of_chunks = blocksize / piecesize;
  if (ctx->number_of_chunks * piecesize != blocksize) {
    ctx->number_of_chunks++;
  }
  ctx->chunk_size = piecesize;

  ctx->k = ctx->instance->args.uargs.k;
  ctx->m = ctx->instance->args.uargs.m;

  ctx->datas   = calloc(ctx->k, sizeof(char*));
  ctx->codings = calloc(ctx->m, sizeof(char*));

  for (i = 0; i < ctx->k; ++i) {
    ctx->datas[i] = malloc(blocksize + ctx->number_of_chunks * sizeof(fragment_header_t));
  }

  for (i = 0; i < ctx->m; ++i) {
    ctx->codings[i] = malloc(blocksize + ctx->number_of_chunks * sizeof(fragment_header_t));
  }
}

int encode_chunk(int desc, char *data, int datalen, struct encode_chunk_context *ctx, int nth)
{
  ec_backend_t ec = ctx->instance;
  char *k_ref[ctx->k];
  char *m_ref[ctx->m];
  int len_to_copy = ctx->chunk_size;
  int one_cell_size = sizeof(fragment_header_t) + ctx->chunk_size;
  int i, ret;

  if (nth >= ctx->number_of_chunks) {
    return -1;
  }

  if (len_to_copy > datalen - ((ctx->k - 1) * ctx->blocksize + nth * len_to_copy) ) {
    len_to_copy = datalen - ((ctx->k - 1) * ctx->blocksize + nth * len_to_copy);
  }


  for (i = 0; i < ctx->k; i++) {
    char *ptr = &ctx->datas[i][nth * one_cell_size];
    fragment_header_t *hdr = (fragment_header_t*)ptr;
    hdr->magic = LIBERASURECODE_FRAG_HEADER_MAGIC;
    ptr = (char*) (hdr + 1);
    memcpy(ptr, data + (i * ctx->blocksize + nth * ctx->chunk_size), len_to_copy);
    k_ref[i] = ptr;
  }

  for (i = 0; i < ctx->m; i++) {
    char *ptr = &ctx->codings[i][nth * one_cell_size];
    fragment_header_t *hdr = (fragment_header_t*)ptr;
    hdr->magic = LIBERASURECODE_FRAG_HEADER_MAGIC;
    ptr = (char*) (hdr + 1);
    m_ref[i] = ptr;
  }

  ret = ec->common.ops->encode(ec->desc.backend_desc, k_ref, m_ref, len_to_copy);
  if (ret < 0) {
      fprintf(stderr, "error encode ret = %d\n", ret);
      return -1;
  } else {
    ret = finalize_fragments_after_encode(ec, ctx->k, ctx->m, ctx->chunk_size, len_to_copy, k_ref, m_ref);
    if (ret < 0) {
      fprintf(stderr, "error encode ret = %d\n", ret);
      return -1;
    }
  }
  return 0;
}


*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

type Version struct {
	Major    uint
	Minor    uint
	Revision uint
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Revision)
}

func (v Version) Less(other Version) bool {
	if v.Major < other.Major {
		return true
	} else if v.Minor < other.Minor {
		return true
	} else if v.Revision < other.Revision {
		return true
	}
	return false
}

func GetVersion() Version {
	return makeVersion(C.liberasurecode_get_version())
}
func makeVersion(v C.uint32_t) Version {
	return Version{
		Major:    uint(v>>16) & 0xffff,
		Minor:    uint(v>>8) & 0xff,
		Revision: uint(v) & 0xff,
	}
}

var KnownBackends = [...]string{
	"null",
	"jerasure_rs_vand",
	"jerasure_rs_cauchy",
	"flat_xor_hd",
	"isa_l_rs_vand",
	"shss",
	"liberasurecode_rs_vand",
	"isa_l_rs_cauchy",
	"libphazr",
}

func AvailableBackends() (avail []string) {
	for _, name := range KnownBackends {
		if BackendIsAvailable(name) {
			avail = append(avail, name)
		}
	}
	return
}

type Params struct {
	Name string
	K    int
	M    int
	W    int
	HD   int
}

type Backend struct {
	Params
	libecDesc C.int
}

func BackendIsAvailable(name string) bool {
	id, err := nameToID(name)
	if err != nil {
		return false
	}
	return C.liberasurecode_backend_available(id) != 0
}

func InitBackend(params Params) (Backend, error) {
	backend := Backend{params, 0}
	id, err := nameToID(backend.Name)
	if err != nil {
		return backend, err
	}
	desc := C.liberasurecode_instance_create(id, &C.struct_ec_args{
		k:  C.int(backend.K),
		m:  C.int(backend.M),
		w:  C.int(backend.W),
		hd: C.int(backend.HD),
		ct: C.CHKSUM_CRC32,
	})
	if desc < 0 {
		return backend, fmt.Errorf("instance_create() returned %v", errToName(-desc))
	}
	backend.libecDesc = desc

	// Workaround on init bug of Jerasure
	// Apparently, jerasure will crash if the
	// first encode is done concurrently with other encode.
	backend.Encode(bytes.Repeat([]byte("1"), 1000))

	return backend, nil
}

func (backend *Backend) Close() error {
	if backend.libecDesc == 0 {
		return errors.New("backend already closed")
	}
	if rc := C.liberasurecode_instance_destroy(backend.libecDesc); rc != 0 {
		return fmt.Errorf("instance_destroy() returned %v", errToName(-rc))
	}
	backend.libecDesc = 0
	return nil
}

func (backend *Backend) Encode(data []byte) ([][]byte, func(), error) {
	var dataFrags **C.char
	var parityFrags **C.char
	var fragLength C.uint64_t
	pData := (*C.char)(unsafe.Pointer(&data[0]))
	if rc := C.liberasurecode_encode(
		backend.libecDesc, pData, C.uint64_t(len(data)),
		&dataFrags, &parityFrags, &fragLength); rc != 0 {
		return nil, nil, fmt.Errorf("encode() returned %v", errToName(-rc))
	}

	result := make([][]byte, backend.K+backend.M)
	for i := 0; i < backend.K; i++ {
		result[i] = (*[1 << 30]byte)(unsafe.Pointer(C.getStrArrayItem(dataFrags, C.int(i))))[:int(C.int(fragLength)):int(C.int(fragLength))]
	}
	for i := 0; i < backend.M; i++ {
		result[i+backend.K] = (*[1 << 30]byte)(unsafe.Pointer(C.getStrArrayItem(parityFrags, C.int(i))))[:int(C.int(fragLength)):int(C.int(fragLength))]
	}
	return result, func() {
		C.liberasurecode_encode_cleanup(
			backend.libecDesc, dataFrags, parityFrags)
	}, nil
}

func (backend *Backend) Decode(frags [][]byte) ([]byte, error) {
	var data *C.char
	var dataLength C.uint64_t
	if len(frags) == 0 {
		return nil, errors.New("decoding requires at least one fragment")
	}

	cFrags := C.makeStrArray(C.int(len(frags)))
	defer C.freeStrArray(cFrags)
	for index, frag := range frags {
		C.setStrArrayItem(cFrags, C.int(index), (*C.uchar)(&frag[0]))
	}

	if rc := C.liberasurecode_decode(
		backend.libecDesc, cFrags, C.int(len(frags)),
		C.uint64_t(len(frags[0])), C.int(1),
		&data, &dataLength); rc != 0 {
		return nil, fmt.Errorf("decode() returned %v", errToName(-rc))
	}
	defer C.liberasurecode_decode_cleanup(backend.libecDesc, data)
	runtime.KeepAlive(frags) // prevent frags from being GC-ed during decode
	return C.GoBytes(unsafe.Pointer(data), C.int(dataLength)), nil
}

func (backend *Backend) Reconstruct(frags [][]byte, fragIndex int) ([]byte, error) {
	if len(frags) == 0 {
		return nil, errors.New("reconstruction requires at least one fragment")
	}
	fragLength := len(frags[0])
	data := make([]byte, fragLength)
	pData := (*C.char)(unsafe.Pointer(&data[0]))

	cFrags := C.makeStrArray(C.int(len(frags)))
	defer C.freeStrArray(cFrags)
	for index, frag := range frags {
		C.setStrArrayItem(cFrags, C.int(index), (*C.uchar)(&frag[0]))
	}

	if rc := C.liberasurecode_reconstruct_fragment(
		backend.libecDesc, cFrags, C.int(len(frags)),
		C.uint64_t(len(frags[0])), C.int(fragIndex), pData); rc != 0 {
		return nil, fmt.Errorf("reconstruct_fragment() returned %v", errToName(-rc))
	}
	runtime.KeepAlive(frags) // prevent frags from being GC-ed during reconstruct
	return data, nil
}

func (backend *Backend) IsInvalidFragment(frag []byte) bool {
	pData := (*C.char)(unsafe.Pointer(&frag[0]))
	return 1 == C.is_invalid_fragment(backend.libecDesc, pData)
}

type FragmentInfo struct {
	Index               int
	Size                int
	BackendMetadataSize int
	OrigDataSize        uint64
	BackendID           C.ec_backend_id_t
	BackendName         string
	BackendVersion      Version
	ErasureCodeVersion  Version
	IsValid             bool
}

func GetFragmentInfo(frag []byte) FragmentInfo {
	header := *(*C.struct_fragment_header_s)(unsafe.Pointer(&frag[0]))
	backendID := C.getBackendID(&header)
	return FragmentInfo{
		Index:               int(header.meta.idx),
		Size:                int(header.meta.size),
		BackendMetadataSize: int(header.meta.frag_backend_metadata_size),
		OrigDataSize:        uint64(C.getOrigDataSize(&header)),
		BackendID:           backendID,
		BackendName:         idToName(backendID),
		BackendVersion:      makeVersion(C.getBackendVersion(&header)),
		ErasureCodeVersion:  makeVersion(C.getECVersion(&header)),
		IsValid:             C.is_invalid_fragment_header((*C.fragment_header_t)(&header)) == 0,
	}
}
