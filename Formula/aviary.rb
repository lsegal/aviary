class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.80"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.80/aviary_v0.7.80_darwin_arm64.tar.gz"
      sha256 "494b548581d2bbb5d8ca3fa4af772a629fb2a99f547cc3d0174431b120c48437"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.80/aviary_v0.7.80_darwin_amd64.tar.gz"
      sha256 "0130f31c1a7480f177cb2ee894dfde527f2db70d96b872e381bb1465e5dfd369"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.80/aviary_v0.7.80_linux_arm64.tar.gz"
      sha256 "9d815b827b685d0f50b6cc5c663d0b3dcb85dae219ef46c25b29975601dcd776"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.80/aviary_v0.7.80_linux_amd64.tar.gz"
      sha256 "a781fd4456e3cf228a92537791029fb6d8d94443b6548a039c998bc8d9c8a168"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
