package demo

import "embed"

// Foto demo BVRooms (JPEG 960px, sumber sama dengan website/public/images) — di-embed agar seed produksi/dev menghasilkan
// galeri yang tampak nyata tanpa bergantung pada file di disk. Nama file tanpa ekstensi dipakai sebagai kunci (lihat bvPhoto).
//
//go:embed photos/*.jpg
var demoPhotoFS embed.FS

// demoPhoto: isi JPEG berdasarkan nama (mis. "hotel-room"); fallback demoJPEG (1×1) bila nama tidak dikenal.
func demoPhoto(name string) []byte {
	if name == "" {
		return demoJPEG
	}
	b, err := demoPhotoFS.ReadFile("photos/" + name + ".jpg")
	if err != nil {
		return demoJPEG
	}
	return b
}
