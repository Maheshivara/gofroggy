// go:build cgo

// C layer over the vendor display driver, used by direct.go.

#ifndef DIRECT_DRIVER_H
#define DIRECT_DRIVER_H

#include <stddef.h>

#define DD_OK 0             // success
#define DD_ERR_DLOPEN (-1)  // the library could not be loaded
#define DD_ERR_NO_DISP (-2) // the library has no video_driver_disp_frame

// dd_load loads the driver; returns DD_OK or a DD_ERR_* code.
int dd_load(const char *path);

// dd_error returns the text of the last dd_load error.
const char *dd_error(void);

// dd_init runs the driver's init and returns its result (1 if it has none).
int dd_init(void);

// dd_show presents a w x h RGB565 frame (pitch in bytes); a non-zero
// update_screen also calls the driver's update_screen.
void dd_show(void *buf, int w, int h, int pitch, int update_screen);

// dd_present calls only the vendor's frame-display function.
void dd_present(void *buf, int w, int h, int pitch);

// dd_update optionally calls the vendor's update-screen function.
void dd_update(void);

// dd_unload unloads the driver.
void dd_unload(void);

// dd_fb_open opens /dev/fb0, stores the panel size (unchanged if unreadable)
// and unblanks the screen. Returns 0 on success.
int dd_fb_open(int *panel_w, int *panel_h);

// dd_fb_close closes /dev/fb0.
void dd_fb_close(void);

// dd_fb_wait_vsync blocks until the next vertical blank. Returns 0 on success.
int dd_fb_wait_vsync(void);

// dd_alloc returns zeroed memory (null on failure).
void *dd_alloc(size_t size);

// dd_free frees memory from dd_alloc.
void dd_free(void *p);

// dd_copy copies n bytes from src to dst.
void dd_copy(void *dst, const void *src, size_t n);

#endif
