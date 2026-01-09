{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  buildInputs = with pkgs; [
    gcc
    gdb
    xorg.libX11
    xorg.libXrandr
    xorg.libXcursor
    xorg.libXinerama
    xorg.libXi
    xorg.libXxf86vm
    libGL
    alsa-lib
    libz
  ];

  LD_LIBRARY_PATH="${pkgs.libGL}/lib:${pkgs.libglvnd}/lib:${pkgs.xorg.libX11}/lib:${pkgs.xorg.libXcursor}/lib:${pkgs.xorg.libXrandr}/lib:${pkgs.xorg.libXi}/lib:${pkgs.libz}/lib:${pkgs.stdenv.cc.cc.lib}/lib:$LD_LIBRARY_PATH";

  nativeBuildInputs = with pkgs; [
    pkg-config
  ];
}
