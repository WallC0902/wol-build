#include <unistd.h>
#include <cstdlib>

int main(int argc, char* argv[]) {
    int seconds = 1;
    if (argc > 1) {
        seconds = atoi(argv[1]);
    }
    sleep(seconds);
    return 0;
}
