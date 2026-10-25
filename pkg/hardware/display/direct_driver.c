// go:build cgo

// Implementation of direct_driver.h.

#include "direct_driver.h"

#include <dlfcn.h>
#include <fcntl.h>
#include <linux/fb.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <unistd.h>

#ifndef FBIOBLANK
#define FBIOBLANK 0x4611
#endif
#ifndef FBIO_WAITFORVSYNC
#define FBIO_WAITFORVSYNC _IOW('F', 32, unsigned int)
#endif

typedef int (*fn_init)();
typedef int (*fn_disp)(void *src, int w, int h, int pitch);
typedef int (*fn_update)();

// Driver library and its entry points (set by dd_load).
static void *g_handle;
static fn_init g_init;
static fn_disp g_disp;
static fn_update g_update;

static char g_err[300]; // last dd_load error text
static int g_fb = -1;   // /dev/fb0 descriptor

int dd_load(const char *path) {
  g_err[0] = '\0';
  g_handle = dlopen(path, RTLD_NOW | RTLD_GLOBAL);
  if (!g_handle) {
    const char *e = dlerror();
    snprintf(g_err, sizeof g_err, "%s", e ? e : "dlopen failed");
    return DD_ERR_DLOPEN;
  }
  g_init = (fn_init)dlsym(g_handle, "video_drivers_init");
  g_disp = (fn_disp)dlsym(g_handle, "video_driver_disp_frame");
  g_update = (fn_update)dlsym(g_handle, "video_driver_update_screen");
  if (!g_disp) {
    snprintf(g_err, sizeof g_err, "driver has no video_driver_disp_frame");
    dlclose(g_handle);
    g_handle = NULL;
    g_init = NULL;
    g_disp = NULL;
    g_update = NULL;
    return DD_ERR_NO_DISP;
  }
  return DD_OK;
}

const char *dd_error(void) { return g_err[0] ? g_err : "unknown error"; }

int dd_init(void) { return g_init ? g_init() : 1; }

void dd_present(void *buf, int w, int h, int pitch) {
  if (g_disp)
    g_disp(buf, w, h, pitch);
}

void dd_update(void) {
  if (g_update)
    g_update();
}

void dd_show(void *buf, int w, int h, int pitch, int update_screen) {
  dd_present(buf, w, h, pitch);
  if (update_screen)
    dd_update();
}

void dd_unload(void) {
  if (g_handle) {
    dlclose(g_handle);
    g_handle = NULL;
  }
  g_init = NULL;
  g_disp = NULL;
  g_update = NULL;
}

int dd_fb_open(int *panel_w, int *panel_h) {
  g_fb = open("/dev/fb0", O_RDWR);
  if (g_fb < 0)
    return -1;
  struct fb_var_screeninfo v;
  if (ioctl(g_fb, FBIOGET_VSCREENINFO, &v) == 0 && v.xres > 0 && v.yres > 0) {
    *panel_w = (int)v.xres;
    *panel_h = (int)v.yres;
  }
  ioctl(g_fb, FBIOBLANK, 0);
  return 0;
}

void dd_fb_close(void) {
  if (g_fb >= 0) {
    close(g_fb);
    g_fb = -1;
  }
}

int dd_fb_wait_vsync(void) {
  unsigned int zero = 0;
  if (g_fb < 0)
    return -1;
  return ioctl(g_fb, FBIO_WAITFORVSYNC, &zero);
}

void *dd_alloc(size_t size) { return calloc(1, size); }
void dd_free(void *p) { free(p); }
void dd_copy(void *dst, const void *src, size_t n) { memcpy(dst, src, n); }
