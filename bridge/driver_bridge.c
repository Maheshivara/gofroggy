// driver_bridge: owns the vendor display driver so the Go program doesn't
// need cgo.
//
// Usage: driver_bridge [--driver PATH] [--size WxH] [--vsync] [--update-screen]
//   --driver PATH  driver library to load. Without it: ./driver_sf3000.so,
//                  then /mnt/sdcard/cubegm/driver_sf3000.so.
//   --size WxH     render size, already worked out by the Go side (even
//                  numbers). Without it the panel size is used.
//
// Protocol (Go <-> bridge):
//   1. The bridge loads the driver, allocates the staging buffers and creates a
//      shared-memory file holding ONE frame (what Go draws into). It then
//      prints one line on the protocol channel:
//          OK <width> <height> <shm path> <vsync 0|1> R
//      (the trailing R means the row-range command below is understood) or,
//      if anything failed:
//          ERR <message>
//   2. Go writes one byte 'F' to the bridge's stdin = "the frame in shared
//      memory is finished". Or the five bytes 'R', row0 (uint16 LE), row1
//      (uint16 LE) = "only rows row0..row1 (inclusive) changed since this
//      staging buffer was last filled"; only those rows are copied.
//   3. The bridge copies it into the next staging buffer, then replies with
//      three uint32 (native endian), in microseconds:
//          [copy time of this frame][vsync wait of previous frame][show time of
//          previous frame]
//      After replying, Go may immediately draw the next frame while the bridge
//      presents the one it just copied.
//   4. stdin EOF = quit (also happens if the Go process dies).

#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <linux/fb.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <time.h>
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

static uint64_t now_us(void) {
  struct timespec ts;
  clock_gettime(CLOCK_MONOTONIC, &ts);
  return (uint64_t)ts.tv_sec * 1000000ULL + (uint64_t)ts.tv_nsec / 1000ULL;
}

static int proto_fd = -1;

// read_full reads exactly n bytes; returns -1 on EOF or error.
static int read_full(int fd, void *buf, size_t n) {
  unsigned char *p = (unsigned char *)buf;
  while (n > 0) {
    ssize_t r = read(fd, p, n);
    if (r < 0 && errno == EINTR)
      continue;
    if (r <= 0)
      return -1;
    p += r;
    n -= (size_t)r;
  }
  return 0;
}

static int fail(const char *msg) {
  dprintf(proto_fd, "ERR %s\n", msg);
  return 1;
}

int main(int argc, char **argv) {
  // Vendor libraries sometimes printf to stdout. Keep the real stdout as a
  // private protocol channel and send everything else to stderr.
  proto_fd = dup(STDOUT_FILENO);
  dup2(STDERR_FILENO, STDOUT_FILENO);

  int req_w = 0, req_h = 0;
  const char *driver_arg = NULL;
  int want_vsync = 0;
  int want_update = 0;
  for (int i = 1; i < argc; i++) {
    if (!strcmp(argv[i], "--size") && i + 1 < argc) {
      if (sscanf(argv[++i], "%dx%d", &req_w, &req_h) != 2)
        req_w = req_h = 0;
    } else if (!strcmp(argv[i], "--driver") && i + 1 < argc) {
      driver_arg = argv[++i];
    } else if (!strcmp(argv[i], "--vsync")) {
      want_vsync = 1;
    } else if (!strcmp(argv[i], "--update-screen")) {
      want_update = 1;
    }
  }

  // Panel resolution + unblank. The fd stays open for the vsync ioctl.
  int fb_w = 854, fb_h = 480;
  int fb_fd = open("/dev/fb0", O_RDWR);
  if (fb_fd >= 0) {
    struct fb_var_screeninfo v;
    if (ioctl(fb_fd, FBIOGET_VSCREENINFO, &v) == 0 && v.xres > 0 &&
        v.yres > 0) {
      fb_w = (int)v.xres;
      fb_h = (int)v.yres;
    }
    ioctl(fb_fd, FBIOBLANK, FB_BLANK_UNBLANK);
  }

  // Even dimensions only: odd source widths wedge some driver builds.
  int width, height;
  if (req_w > 0 && req_h > 0) {
    width = req_w & ~1;
    height = req_h & ~1;
  } else {
    width = fb_w & ~1;
    height = fb_h & ~1;
  }
  if (width < 2 || height < 2)
    return fail("render size too small");
  if (width > 1280 || height > 720)
    return fail("render size larger than 1280x720");
  int frame_size = width * height * 2;
  int pitch = width * 2;

  // Probe the vsync ioctl once so Go can be told whether it really works.
  int vsync_ok = 0;
  if (want_vsync && fb_fd >= 0) {
    unsigned int zero = 0;
    vsync_ok = (ioctl(fb_fd, FBIO_WAITFORVSYNC, &zero) == 0);
  }

  // Load vendor driver
  const char *driver_path = driver_arg;
  if (!driver_path) {
    driver_path = "/usr/lib/driver.so";
    if (access(driver_path, F_OK) != 0) {
      driver_path = "/mnt/sdcard/cubegm/driver_sf3000.so";
    }
  }
  void *handle = dlopen(driver_path, RTLD_NOW | RTLD_GLOBAL);
  if (!handle) {
    char msg[300];
    snprintf(msg, sizeof msg, "dlopen %s: %s", driver_path, dlerror());
    return fail(msg);
  }

  fn_init video_drivers_init = (fn_init)dlsym(handle, "video_drivers_init");
  fn_disp video_driver_disp_frame =
      (fn_disp)dlsym(handle, "video_driver_disp_frame");
  fn_update video_driver_update_screen =
      (fn_update)dlsym(handle, "video_driver_update_screen");
  if (!video_driver_disp_frame) {
    return fail("driver has no video_driver_disp_frame");
  }

  if (video_drivers_init) {
    int rv = video_drivers_init();
    if (rv <= 0) {
      // The stock frontend treats <= 0 as failure; only warn, since it worked
      // for this driver before.
      fprintf(stderr, "driver_bridge: video_drivers_init returned %d\n", rv);
    }
  }

  // Two staging buffers that alternate: the hardware may still be reading
  // the previous one while we fill the next.
  unsigned char *stage[2];
  for (int i = 0; i < 2; i++) {
    stage[i] = (unsigned char *)malloc(frame_size);
    if (!stage[i])
      return fail("cannot allocate staging buffer");
    memset(stage[i], 0, frame_size);
  }

  // Shared memory with ONE frame that Go draws into
  char shm_path[64];
  snprintf(shm_path, sizeof shm_path, "/dev/shm/driver_bridge_%d",
           (int)getpid());
  int shm_fd = open(shm_path, O_RDWR | O_CREAT | O_TRUNC, 0600);
  if (shm_fd < 0) {
    snprintf(shm_path, sizeof shm_path, "/tmp/driver_bridge_%d", (int)getpid());
    shm_fd = open(shm_path, O_RDWR | O_CREAT | O_TRUNC, 0600);
  }
  if (shm_fd < 0 || ftruncate(shm_fd, frame_size) != 0) {
    return fail("cannot create shared memory file");
  }
  unsigned char *shm = mmap(NULL, (size_t)frame_size, PROT_READ | PROT_WRITE,
                            MAP_SHARED, shm_fd, 0);
  close(shm_fd);
  if (shm == MAP_FAILED)
    return fail("cannot mmap shared memory");

  // Ready
  dprintf(proto_fd, "OK %d %d %s %d R\n", width, height, shm_path, vsync_ok);

  // Frame loop
  uint32_t last_wait_us = 0, last_show_us = 0;
  int cur = 0;
  unsigned char cmd;
  for (;;) {
    ssize_t n;
    do {
      n = read(STDIN_FILENO, &cmd, 1);
    } while (n < 0 && errno == EINTR);
    if (n != 1)
      break; // EOF: Go closed the pipe or died
    if (cmd != 'F' && cmd != 'R')
      continue;

    int row0 = 0, row1 = height - 1;
    if (cmd == 'R') {
      unsigned char r[4];
      if (read_full(STDIN_FILENO, r, sizeof r) != 0)
        break;
      row0 = r[0] | (r[1] << 8);
      row1 = r[2] | (r[3] << 8);
      if (row1 >= height)
        row1 = height - 1;
    }

    // Copy the finished frame into the staging buffer the hardware is NOT
    // reading...
    unsigned char *buf = stage[cur];
    cur ^= 1;
    uint64_t t0 = now_us();
    if (row1 >= row0)
      memcpy(buf + (size_t)row0 * pitch, shm + (size_t)row0 * pitch,
             (size_t)(row1 - row0 + 1) * pitch);
    uint32_t copy_us = (uint32_t)(now_us() - t0);

    // ...and tell Go right away so it can draw the next frame while we present.
    uint32_t ack[3] = {copy_us, last_wait_us, last_show_us};
    if (write(proto_fd, ack, sizeof ack) != (ssize_t)sizeof ack)
      break;

    uint64_t tw = now_us();
    if (vsync_ok) {
      unsigned int zero = 0;
      ioctl(fb_fd, FBIO_WAITFORVSYNC, &zero);
    }
    last_wait_us = (uint32_t)(now_us() - tw);

    uint64_t ts = now_us();
    video_driver_disp_frame(buf, width, height, pitch);
    if (want_update && video_driver_update_screen) {
      video_driver_update_screen();
    }
    last_show_us = (uint32_t)(now_us() - ts);
  }

  // Cleanup
  unlink(shm_path);
  munmap(shm, (size_t)frame_size);
  free(stage[0]);
  free(stage[1]);
  dlclose(handle);
  return 0;
}
