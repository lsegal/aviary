class Aviary < Formula
  desc "Aviary: the AI Agent Nest"
  homepage "https://aviary.bot"
  license "MIT"
  version "0.7.17"

  on_macos do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.17/aviary_v0.7.17_darwin_arm64.tar.gz"
      sha256 "493bd9a22dbc8e7be1ae99b892d1973459dd6bfe1b78709222a5e781b8dd7a80"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.17/aviary_v0.7.17_darwin_amd64.tar.gz"
      sha256 "040061fb441c0caa15c250c7792c0285f32568f8c66e45363d2c6a4f31c60833"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.17/aviary_v0.7.17_linux_arm64.tar.gz"
      sha256 "eb052b3ac4916c8ee2b01e562d07ff0ccc4da94af06019b808ac171cfcfbb037"
    end

    on_intel do
      url "https://github.com/lsegal/aviary/releases/download/v0.7.17/aviary_v0.7.17_linux_amd64.tar.gz"
      sha256 "cda8f90434d677a174b6662dea22bc71f54fbb49844e5c7f107deac067325898"
    end
  end

  def install
    bin.install "aviary"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/aviary version")
  end
end
