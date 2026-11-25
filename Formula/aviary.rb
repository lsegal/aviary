class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.49"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.49/aviary_v0.7.49_darwin_arm64.tar.gz"
      sha256 "9d2a3dfcb3dbbb13c77782c4d94327d5534d15552dff8736fc19dee803fa7856"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.49/aviary_v0.7.49_darwin_amd64.tar.gz"
      sha256 "02677e29a2ebce165606ffa03719c028836a61f09a0b330f95f96a3f26a54d31"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.49/aviary_v0.7.49_linux_arm64.tar.gz"
      sha256 "894bd2b1d0d3286e6d069f4cc40046aa8e74fafe2d89bb82e51fb28ae0939030"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.49/aviary_v0.7.49_linux_amd64.tar.gz"
      sha256 "b58306ee726ba6af9fc0607ade833ab3fc710c967ac560b39c05c4694489d1e5"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
