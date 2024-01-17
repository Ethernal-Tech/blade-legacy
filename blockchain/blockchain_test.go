package blockchain

import (
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/0xPolygon/polygon-edge/helper/common"
	"github.com/0xPolygon/polygon-edge/helper/hex"
	"github.com/0xPolygon/polygon-edge/state"
	"github.com/hashicorp/go-hclog"
	lru "github.com/hashicorp/golang-lru"

	"github.com/0xPolygon/polygon-edge/chain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/0xPolygon/polygon-edge/blockchain/storage"
	"github.com/0xPolygon/polygon-edge/blockchain/storage/leveldb"
	"github.com/0xPolygon/polygon-edge/blockchain/storage/memory"
	"github.com/0xPolygon/polygon-edge/types"
)

const (
	B                  = 1
	KB                 = 1024 * B
	MB                 = 1024 * KB
	GB                 = 1024 * MB
	transactionsString = `0x605dbc46a1512ba27b617322ce7d7793daa1c8303e64cb0a66ca2ce7f77100d6,0xc1cccfc7027e0746aec594c6fa79c4bf727f03d79287053eb73e4c9515f95e88,0xe5a2b1398f33f6c834ba97721f1fb13c1e783206e7817c29ae028753557b6411,0x8402340ce5077462c4eb01a895e6fecfc831cb1dd74ac40f923367a68af9593b,0xae939e970b523b1d6ba116fc9bf53f9831bdde242044f64024bd3bd7587bcb57,0x8c2a6c643c96e64ad085682d365839623a09129fea3f686947d70b51e2c0e117,0xf0a50535bda4656e5d908c168cdd28d2bbf2acca93559116c0f18de7028bc441,0xaec639454a9165d686e48c51e91beb87187e8d6284c8777216455ba58a5c17be,0xac47c65b79772e7731eee45a746a74765508fa4fc827f2517f165a07fbb5c4b1,0xe77d8f1751f881520bfe73dad6f1a137a0ebc796677ef965124ffee699cf3648,0x2f159e3c637d40f9d3cf8f54d17f042620d8e29a18774811dad6c4594398b301,0xb0bf5d386f6fe5cf2e988c2b547251200a62e05426c7b1fd41ddb6a03e17d027,0x9593043c9ef5affd818266af78e5cdb4516f8790114543cc36ec94059b301d41,0x2f585a28b39bd5433abbfdceba5549b74d67ea0aa6aaa2ff3a743363e9c14dae,0x6010b16d93e63d2a45b9fb961e36f785397a3b915cd272267c3f0c1073c0dc4d,0xe0a2472ce6ec60f0fbfc4887c29cc058abb83e10b1938474b43360f9867dfe25,0x0f49fa7942aa0a278c7ede7ab13e981c2161f088c9cd60040d2c4ab2deb07eb5,0x9fc8a013faaac1a866b0d711ac60fded092aa76eed9ff07148c1aa44d9bd7ab3,0x3bc1942df72d89096a804da2384b5e26eec5fea51760cd8e7a0a569abcb194ef,0xd0adae692cb11a951ec671686dee10f1a063bfb6f2b848c2564f3849860b07fd,0x4b00ca46d2fc4b47cf0551d089210e359b9873ab023b495ba34fe8e282708311,0x3244534ddee2b804d9468c435cbda40fd07159557058a34c80ef76321839cd2f,0x8efb9b5782aecae914bfa8a49c7bfe2af93fe857fff88def513eb4eed174f4ea,0x1f15ced29a53876b5ead22e455d4e128bc4ba0709cfbc28bf8ca7935e486a95c,0xc8cd4ecc00b79738ee1183ca47c84e896a93f6aec47f3dc60b04a7c9bcff28c4,0xc413a8a4c64369e2a88f8b44915f903b4dc6de573e784800f97abad833de6c48,0x9bdcb7bec3493b89ef8c8bbc64171dde2944897a01b2af1694d11b5c87677375,0x9df5862b83e3160ff1207164d579509f85e6c06f5e738c8d586e32c99d98adc7,0x54d273e02cbeba36673110f7d93d8ac4076110cc149945a3943949851417157c,0x5dea3c52c7693f113735d9deb081f08af6f2cfe17b607a5b3ee2825013e73d8d,0xd02bb878edfa884e675f75b716e07d5fc4101413a172cc485dfb8712d36f92d9,0x31fd295056ffa1463bc623222702bc75939a90e637d4a766c1815c59374c1086,0x33524cef7b35f96609b333afe437fd7065c3d0065739a35f1206ea8ddfdb29fe,0xac5aa8882f6e121044539f1b8f7e0d512ee06494a27cfe682bb104a9737fa9c5,0xd28622bc8127257a4addc10655b15225330cfbbb759541af480f403c5a3514e3,0xf22cf33987ad688f4ddad850bd7fa8e5dc7890715cf93de70eb7b6ae77545021,0x32fa0cc360889215d8a0bc3bd86fcdddd296abb4e478abea3ece63f9a91b0002,0x74d456f398673013937fcd2852f7abb171aa8a0a6751d1aec596e7ebe22875f2,0x63777c13600bc2c256c5d51f2dfca1d3da5c38a7e6239b163b9e35052e6981c3,0x1002e482aefcb6a6b3dd621de3657dcaa726bfbc9eb38da34971512cfc489edc,0x93e5ae3816087169ebd767eefb3c31cc0be385e4b938524dbd2f1ef32a23cfa0,0x934bc4cabd83246503ec5238f7d6b5ce4579f9544d7e2d76f485f1b4322a70c6,0x4c28c5180f0cb955ea985a79547d86e339509645e30e110e2a3dc6002a11c88b,0xd7a2baaa80f1cc00d29f489c1770e5e8f03f56c1e25f46b436371d3b1fd8fbff,0xf2f270999af7532a83cc9e66840e40e6c63a9e03a5a500c3385787940c9b8386,0x3b693763ed119b2dbe65cfc8e9b4f0793f4c0a972b29870f3056fd6518b96686,0x2d77a002592458256f4be0e56d799c4b2d2fee29e86600047b0a4edcd6377aec,0xdad0855425363f25726bcc7edc7c373376cb970792266e58348edaae4cf2a7a4,0x4728daf72939bb19e50217a7351f7f0425072d61e460e1764d739fba5f3f6eb3,0x23452ce4b2831388256def5435bbd4f185eb6d7aa3fda51b41d112404a418fef,0x7eebd127f3b327e997728ca995f999109b4d76b217aff8eb721946354accfdc8,0x56de06811ca466778f5ec46543f3c193d1ceffe2f995e400a0742402bb9ba99a,0x4602362af841cc44e51b260f8a2556fc53fcb970278e9316f4185d9a6b77b686,0x4fd77cf9653687e7a1f098b157305723de55289e805c9f1a91875cd36890fafe,0x2cc9b32f78fac05274a5b9f3d8506d7084ad94e9f3995c9fbbe69498752c1070,0x633b6283830373f1952529bfb239faf07482ce275247d10d32c98db366da127f,0xdc90fb1cc066e588f34ddd0641c9deefda2146359181330acd9504d0a36b5e1a,0xfa2d2ae020590360a9e12e8f7ddf594fb8c4133ef8849fcdf0d9d87278dfa083,0x35cbf761d5be02b8ae5519f437ac521e5bb99fcd58c9cd61f8ca97f0dfecc80a,0x35b7f6464e91709f10b22630fa1cdf0aa8c7659689771fca6efbff7a9997ca9c,0x032debe54466e4504fb4e7ade11e98b0541f3466ccafa3681aee9f11363ab830,0x60f272fbc0163426f3d2b730ea8a6ae8d1bfa8c5b7ab6995637d2ae3b11f444d,0x374daf61c108c4a71f0e4761b3e1b892a98f80eb13eaa68127e37a7fa3b7353a,0x1ab4686f37b86fd30349f16ce14a8ae61fae1d512fdbe718d0330775b7a252a3,0x65dd3109ee557c32633d5f08243eaedfa5e0ff0a860fcd51aad1433d1ef02d44,0xbb43446db56520cf2169979a41bc8bc20e076f9d27e1d676ad3178cd53554e68,0xf7dd48583905a560fe4336429cb8410e554441bca3186c95c60769625f061a92,0x1d673baea03b14a3714e2aae1d0b1861ae907675aa5709757d5d43026fe25932,0xb8473890f8c5f090f292d8b90d84da7933f819f9c5667a1489c2c5807172d643,0xab91d2f5dbd20505f02969c3aebb5f2cf42d9bc4eafeac5064b874fc66f9f37e,0x15cc8f34c13c3a0af1dc733a13f675e06f2a384d0d0fb96b8acbfe10106099d8,0x51348fc98adab4b36bb84d80c782530ed21833848069dc2c2ba3727a74a855cf,0x1570b2c94330c9fdffa64bdd919e186d97bfd7caa503ebbd391b102f6425e7e4,0x364531b71fa70cd8b4f40b8f784a6f0416d93763ede305068fe6e4ebbcf329f5,0xc00f8581dedcccaa308ae3a7ea68a437465e8f81df8ea2d8ffbf0888539f90bb,0xa9e0d849551307bd7324e5ff59bcd7ac9616212d31fa23de8a05f0ff9599d755,0xa6ad856ed3333960380c4d7b27b3a4ffbf250d5b3f0e7a17f1d8c144e900d728,0x578e9f180cc5588f1df7b928c73f82b06a09b68e92d36474f57b331d2806f472,0xc23b8bf599e876d5bc8208479fcc21d0069f53dd47ef296877bd854b267a89d3,0x6238cad01f155a3834fa999c5e9a05df5b436a81a6ef2caa692513133e75edca,0xfa0818f9c8298e03975a800b8f587aebc70c8364216ce2e04a3b3ea978e2db73,0x0c11bac583a46c407b0454609fdc501d8da82f01aa9f61815b41121703911537,0x15f1c69dc366a192d393ed948940747db1ca176d2c7623d661614dc2041f56f3,0x82010b399f0f32a7a7202e61ed24e679eacda1ba17f29d1f0502caa7e33a596b,0x91d2724f37538a941630ccbb780b5a44258c5329ca1bc3bf2808608864a99d00,0x88e2938adb8c089959db6862d7b423339958d13acc15022c66f7ba245e7a23ef,0x8612a5d83a4b6414c64aa16d75c7c10cc45c41679aeb299cecb5cbfa9c6a8d13,0xf5fcebb020ac61ff5a91681afa5ece0875a4876bf93396efadbbeb2df5516b60,0x5fe9d4fb104feb17c51baf6ca5db336fbdf1f918abda6815c6060da13f51f14f,0xe2920815f1fd18c88e6396c244aac21c6f3fb9f84f7b328250b520e3a084321d,0x0868d0c57b23b64fb9fe85c55fc65fe80c331419eaf1064133c6a8591d4d1d17,0xfaae5e53a5f9dcd681381f00986749ec0d3205b22baec8595dc80b6d3a9198e3,0x25c0ce4330c04e6d596e64741282333358fd4d6eddf22ace36b1939b6d0f0f39,0x2c68b3faa195f657d2cd066933b64fbe5a29efcc3be6227b9e1d0172bb2a1633,0xdfd5f897eaf4e277280859ddbb3bab3d4c7f55fa87326773358adad2ad613e5c,0xc77b4d8584ac24b68d6da2eae7762aa4695787997742c2259630e0d6994a6327,0xce978698b2219fd33c058b625fd1cf8c0efdb606e5aa5e9117ec347222b8a153,0x0d9da3e6b505ddd3fef6c77ff014757a6426af467fbf0ebc7099cafc5c03a26d,0x6ce5db455b2a4420b0cebd6950f2a5bccc838f6af3404e8b9603b66d81653019,0xe1a53b9be99311fb10b9d0c0c91e9c04537c24a54a427bc1ab1d16d602f4c499,0x08462c41a9a9cef598ae0631c8c6a214df63d80350693c74965055f1c54f78ce,0x73d6306e77b0ee22383c62a02f40c5c2635b3e65f2f524593d3d99ee16e4dc56,0x315abdf6cf91fea35315724a1f4c13fc54a7404dc3498147b4554796b83a4d6b,0x0bd9a4830b74a50727e74086ccc024778fcb75d480a5d66dff21707ac168f1ad,0x0ac7397526871e98ebca7244c1d92fb7f4da8b1643c6048808fec85a53936566,0xec6fff6075a9e2ea188c46eadd69fadc10f211f354c70f4470751b28d07ee879,0x509ad5421845cfbe093feceab66c43f4a14547fb167bc1c6f6b2365cfe57d578,0x0501cbb0c3f71fcaf68ed5cdf28bdf91cb56cd620abfc7b2008e644739f20270,0xea51828fe7e28b5f7d878506850984e10615872a2e89122744c76de2f3d63cf5,0x6ce6ae31d11dc6c335689f11329181e6c4112fbb9d8dd1557e3598d822cf200e,0x86c2715ca6de4b42b66bb31d82bbc7931eefa8b3783c0782a57bb768479945ab,0xb6d6dee6af508a440fb32b199c5903813211323e7fc7ec8093718ce8836297ef,0xea95970aa7297b4578ab61f5facfa70eadfd98c4e289973eb11fd3bbac90aa5c,0x2b61d785037bee09cb2b2a94faf66a52abfb1d8b9ec375df54a5937b485b5b6f,0x10e751a85eb1f6a7aa0da4c6a7f65431d7629b3e362a25b9cf93df44537ba008,0xb35b983878ee9792cfd9c32a99255e6e772b5075eece1959e40f9020c2cc65bb,0x392cc142f5695750b367aebc32ad5092b201453bb8d1a6a316794957535fb679,0xef022e9107c04532b14e099f709fd72dc1a2153b88e43a3a04996021628c4457,0xc31662da3bebb2d8addd9627b69a615ab03a0a8abb9f384462f79cce4f046dd7,0xc1c059c2acd6ae2de3d077279abe56a5cee73d99586c50e1e1509404d4590c70,0xd366deadbcd78fcb5dbcb7f6e1e96bf2c9a84d274479e1d9766091b529f3b09b,0x6756b244b7c305e19ef4eda984970b01431ec39dd69825c24d518516c5ad83c9,0x63fb12b7dc6db8b8c92517ba086045fd35498779f4d713a4ca5515c299849b6e,0xa4f25eaa3d7abf2f362c20dbcb2435c0c9c75728e19e6a506d88eb4ffb899ba0,0xca4c6e545e9f0cd3caf71ab49c6852ba69dcf1f297c240730e879ee717c67e94,0x4bcfa016a56af4939222d1b56e165673ab5e97c96bb38df3621ebe2077b8e7ce,0xb9d52548d7355846d38c0f909fed178a4f438465c5c1c086e422c4109528a966,0xe0efa8df833e9375e5b937f13c36d4d27537ccd0ea32f593a12616d66577b490,0x192a24b85af9a52cf4a8dd83ee3ccee9bac33e9c95c15e400f2a4d4db443362f,0x1912e0d6436e1f70c5cad81af5f9f723661df9e6d8ac837ad1d01b466c13ebbd,0xd00d12962038b0682dd4048221a3a891d38a62e22701758d70be3fa4cab97911,0x033b96621a1fd6e7d17fa7afa64027b2e548957f94ff486c89e170dd84a3b9bf,0x865722a39324befd43abd0cc250d0d3a5260c90d08b5982309fdc3eaf9e82feb,0xb51e0dd7f9961a2d353a7f403b343550d5401e925f53f738e884c6d10eb4e83e,0xe7027033fec50dc0acca12a27b6f34d1ad807ec399864d155899e9d3760d5aef,0xa30d2ec05d36d9fb8b15e746f9f78fc014784f7b0379502512fc1aa98337dc1d,0x507957b249f926b5c152e1a49614ba5e09b4579c1d9c74d08bac25b16939c018,0x222ba592d81b577f0e9591524d88a03320676c5e4b23e0440d98b93981b0d5bc,0xc653b8770ccf91c626c9ec531dea933fa4f6a60f0497e2c72bd7e4c58d28e3eb,0x45afa2d6e684d54aad3c9756fd11573d047b8154ee6f1f2e9ebde25fd767544c,0x690e255aa80982ffd3ee34e75924c09419f38fbd0280700352004dee1f658101,0x3707133b46d431e5ad8b54d0e045205ac9c3fb372f7ccc6db11691ee12cfc41c,0xc788bf85aafde8b8cde564172118f63a77e48ddb1dfed62494d8bded5aaee812,0xe4df80fe4a77b9bf96aa8c37ef0c5737399ba86c826a513d1d7550765e92eea7,0x6b285e70ec91ad1c3dd9cc0183eb9a92d486a1a203ead9b8941340e42905fd6c,0x0bfd297869739658ad60fc5ec24aee04c5a501c8fd8cabba268c928f6fa3a19c,0x36b58c5bbb2618cd7fba2fac2365283aa0ca621adc08ceacd15a6606e49a77f9,0x8e7af32c04df4a18762f0fc833d3c22a2b71da9ff577cf28168accc7bc5e777f,0xe23168f34e106ba22bbd208cd99d0447f93734ea5cfe2883d79f3cbc3f721b48,0x7b1c0eec31a271925294af692fd0f9fc796e191520e58ffc016f2c75fee31dcd,0x22dcc102759569e52f7a2194eace75c6b4a9c5eb9c68156d8ba2498c38f57043,0x306c4ba2d6e1848a6356fb6cae5d23b9675d45d8a24c47b5036a427bc0219ba6,0xe398b32311575d6f25fef7a4b7c6d9265844e3d8b0941630afa7612f1cfad817,0xec6f2881a526c94248088c516238578a3ca678782c898cff44f274b108556bbc,0xb35236b01c1c228d4cf6401801970907afdacea3045afd87c08fe14d4925b474,0xa49d5a3cc268d543023faebdd81a8002b8ea690f2f6f54583a9ca23c9de6ebf5,0xdf3ee9bb8975b75801d80df7b48f5ce3af7ef7662f78a86b47b197f58de763f6,0x97af50f712285b5529038fb351239fdb4c14a34c839020ea3deb2dc7b4ef0628,0xe6c220f81b17c6baca3da6a604deae248135737218b66e1af5babe177e0e68d4,0x3524fbbbacb17d9e567d8a50817d3b71f48c30947ce4ecaccd415e66d11e82ae,0xa944ceb4b26688daee1627fc9899d71513f41fa4b0442ecb0f428125c347449c,0x49c35eff0ccdd43ddbbbec538599693cf94f8439ca38fdc925699fbe3a2f949f,0x943077c1af0e85305ace19caab57223b180eaf64ec65f2ca92d88d8d4bf7666a,0x049c402cebbb15b2b35a75643c38a05c8f65f80d4a4af385241fac9bba83235c,0xc2b134b95e26c92afab4beb54bb45c3b73f50401b040e84297aff4134552c21b,0x6790f3b3af6f533889a27ebe105122cb65dbee488ed83112edfe5fcb3733dbc5,0xdce6c4f29fcf89c7d30103e0d5239cfb77818aac9e9a92f9769de8fc1ff1c07a,0x8cec605ca779602b3a4fba9e955e33723a9e8bdcc60fe3200e855dd160579729,0x26b5c225934c1da260c192f91276ea96702fc66a8377bdd7a9f670a732351f8e,0xf2abdb163cb7c2f2d447001d37f5e200328dee7b65bf889424fe507a2ec7a75c,0x296720ac103b55cdedf0da46e5f0e4d1eb8bb15bc7c1e7766319575cd94cdb18,0x5f4e47dbfb48f11324d749a79779b3fc8d5d02e3dd9d87ae25c4590a31c64e81,0x28843ff37fedf35de1507a2717af94acd4911ab16e613c4f623526576442457c,0x2a43974c6e0e1dc0c8d8144eafdc6a74747f2100d32f3eb657fe60594f3a8853,0xdc577139a8da23462e203ba14bc4e4c12e1d04291d74769ae953fa02d2d535b8,0x8a9caf264162511adc549ae33245fd83e583d0c32384e0d3bed483499dd53b9a,0x1e2ef944264bbb5a5f14942acf805cc38b3e1ceeb36919df18f4be35b524ca7b,0xc98fcbb40ff8c1d3c7a383b25fb20cc3a25e6603d6a4501aece24bdd2365c895,0x2cc436da041d68e13f7dca7447bc565a34df2140120116312b04b763bdafa065,0x060957697576ce3e03c85620adb5099acf5b247e6e3c20bb7d56746dec73bdc9,0xeda2a532b49c7242952685c564f95157bdc0106713eaa3a8f13ab32de1c38868,0x64e08d5ab2f21fd2efd74cc88bb60edb6cabad24a124ea04f59557d897b942d6,0x22d8f35759315c99eb5de0841371459497788877b4d361fbd57877ea55c557cc,0xdb1dbf30e96394333655a4371ca80b260cb0648ea99fae159b05ad32bd6cfeca,0xd640d6ff2b506f7f899e287667d478e364d2d334d2af4135948dab68717342fc,0xc40b97798884a7efee2a8192a07d29fbf90190864e5c6f7f8f8cf19becf8cb10,0x684027e531c270233257b97d7eb17fa75f3fea4406e37d313402e7a149c6891a,0x6a87c9966fcb9d53f4e5c23b5e7b434ff2dbbf8829ba407d9b02b955cf72dcdb,0xb73680f6d42297bfbb1ffd2099dbff1941dc820fec9f08f6486af895822dff08,0xd939ddec8c1d7219033b649a55dc417c38b38d4127e9df69ab425cc08c879d15,0x8cf01e4c745de17821f3208ecf4c7508551ec4ad7873d28284ac232e2ba2a1c1,0xa2d9deab4dd7ce07ffd305640c862d09e6fbcba9914b22032508e6f09c8f99b5,0xd463cffc771ae5b5164975fc9bf9015a3e52e6bd53ce9604a61eae4949b36f30,0xb66940e7ffb0be27e4e79f29293a64a7bfa2645224cdfade4c283582d77797c9,0xb2684f5fb905843561a9b192446efd1338f17ec42be95c054e4c3c715823b502,0xa29e772d9f3da81382b36826f0053ec2eb07bf464d64c95c7c248eeac27e97ee,0x638f43f962f75b174c7de9f69014c360006620ca74a01de95a467fd18f377684,0xf83520b4aa79e9eb45a53aaac09f5dc6ae58603c8a6e2201f8c6bad9b499dd27,0x51b8752a2575700ceeab51a07207ad3ca4a4190039c51637f6c1c387e9420863,0x79e99bfe06d3b5cffa5333fc7e01852e8fdf57434f2309fd2bcc4c68a38a2df9,0xd38b144c4c7dc9cbb18bc165427229f5d15beeb73685a4ffe6117d4cfc09e5b6,0x311725787caa3a05ffb825b38218c74c065630a05dc98eb1f5341db4936b2b09,0x26af97c4ce98a7750eb9e0bfc342e5b833f6bdc0611199c5de8250dacc687def,0x41361b24862efc0aa2a77207e344304dc39801f6045320661a3bddb4ccb24918,0x44b513bbe3555ef3cedc6c665db2ee548d7c42604636fb84a3b9e97e10e9668a,0x1e2d97527c684be88d2b27d8c0238a335435cb96dc4a4d232d3b7b8f8fdff0f7,0xdd975d7cdd983035e92452b4f943f05d0c9abbd9dbcbce91fba2f98813543fd7,0xbccdbdb3db37f2220523fba0fdaf1403a694d7a1596836d6b9fad8fec3245d70,0x08d4f903be9639c5837230b178849bbb874924f87c889845da6bab78c8986ae7,0xe5329481cd439a0b8e98f753079d59708772dbde07c83b2058ca8b4d2ca41292,0x3162182731e3b0a07996f0512b9814889bf79fe0eb9f6b5fadfd93d29138566d,0xc0dbc849b6d9f18d637c9fb620cf3ccefa32f5fd05168b7ff28d92c0e98ead45,0x41c336fc854abb6b34232794ac2e9cf9d5d9ef096f7e85bcb0aaf3a344946ce8,0x7eb37b6894ce2c5afd9d71aabe13ea11bbd3197682e7acc2ad0378237a86f611,0x099151c34a8b1ab0a4c5253952b34df5d10c7e01fa9ca612c6a2c60c24e4f9f4,0x79919351a33f055c3cc2bb26056891a2a74cabc5d820e27b2f25c39353810160,0xee77255ca0a12a5a7a80089fd487442add9e67f96a4f3f042e38b473876f32ac,0x3f0af60a350c17d325547115e504619ce13a2ad67003e5030bfbcf86e08b9d98,0x6b966999f54cc2b462a78a3c46fccbf65572dcbebd9fc20896e1bb6d8d927241,0xc0cb83ca5912c3a254663b311a55dd6c3a0349fafd3f9c1d7216350428ef2c74,0x5c34557b20e3318a4b46ec0b36f5a2ffb9b5310e3fd5fcec82fc71f909207dd1,0xd4665b73dc190bb588317ea7a9c14641284b3575dc7e371d2eb810d34fc70f8c,0x0ffe655a0a74899a7e4c96039374bda138a00146cb19d2fe6e6168acb3226102,0xd0f0dcfe4f42a427b460991d607ef9fa9fd3981ded21c996cc5377d4d74ba955,0x8e5efdcd4fb9fd055fb945d0db3ec13aff53da2cb7ab021f31cb341e7a693ba0,0x7252c729f3ee9c41251d3539c0ad21b517ac0aa321a590c2fa3b8e834310d4a6,0xf6c2208d825170b35e3cb0581f1a5d2efb4fa3cdf0451b1896cbf9175f501c49,0x19e37980f350fb8bd8caca7ac447d91362ee9a1292e3cbee0d2538c4ee76c1a5,0x1ae37a57a58665174bb9039b747082ebf70208bd514a82c6a1517975c182b432,0x55de9842a175daf64e09fff790fb6229b619bc1aeeecaacc018d761130117fbf,0xbf25c6f94dc53b00e1131bac91a98ea9372544e958adbd81c8f78107114c0095,0x31e191a52196696ddea85c24069fafd168987b2a09e08e666989afb8155e0dd5,0x4309b5d7eff3543af1aef7a77182829516e94753cce05765c2e4433fd244cb1e,0xec0c7a397028a428e566ece2cbb277adf0aedcdd5fef8701f63ce1765d44561a,0x5509fc3345f73528c7411ed8acc3ea95e90c725ec05e761f39a31c7ae171e408,0x7529e2b87ae19d80d94137123637fc5832612971afaa7ae63605a6e86b6b9ebc,0xf699ef1ec7e44fea5e2e3ae9dc13d8139fc6e1d1a250c7f148f1f59e9b10ba29,0xe30f9f6d8c7fe051482aef4593091db792f995f7c9018acb5383e393d8939648,0xd1911d37284fbb555f02cb5c18bd7ae572a0b53336d462a0eac95c39c36a8de7,0x422d6fde36b19c80a80560eb04237644654fe87a178e01b5f982648cf868cfb0,0x163a05d1f1dd2f0990191d0e8cf30bbacd2fb10d022e1cab18bddc067915637b,0xddb694b150692aebff2e672d55b632aeae78c6296b08374988038e7fff77bc84,0x4229437a02788f82fdcc92403bf242e77221bc05db45cf4ae345f2946e6a2738,0x56c7cf7987effc271152646a9989575d6314d5a679b088bc844a4f9886c46bde,0xe5bc7adb567cc538cdf486747d1234b0d24fc96906d6ec9f3cb09416ca4bb473,0xbf05b88ed6b94a14b01805b5320235bcb78565a6bd10949368b0afd49238c122,0xdf6ccff082cddf2840dc4eb435ced27029efd5898dfed087e2ced966c3e5358b,0xa1fd01b60e011a6ed3ae6324b2d8f6f17ff4fc2b3315c3f7e050729d3cb5e553,0xafd7fca35d9f2ab98489362a9544e8f4878f86537922cb8fcaa9c0f6f88cb9ff,0xc9ed8776f7e1e4a29b94150a9e1a031d1a79c7ffe2b93957df4bc1e9c5840509,0x8a87ca910b511a8ec3bc7116a74b4338277a8f2436e0593e61fca6531cb242e5,0x06f5b509260b318c10a2730cf57a4f588c2a8ae08e937639b62efdd2401c6b98,0x4f29fcb04f5d2d8b219d2e8fc08faeebdf815407e0633955d4d5da9c915f7fe9,0xd7b6dd3305d04c0d69e24c0335a4b80979523f9691201c5997de5b4c30f21f7d,0x1b7949fd917a97a7862f7cfde4b6108de0d0fde15c4279878d6912efd5585db7,0x309ee619b3dfa8712bf5093f71eb817fc1a273a8298588b077997cfb67617249,0x7d725aee40575e11975e8e65001b3495fae5b944002aa3e2319ae8b54a7340c8,0xe2d351541fadea2994fe34000f297e870ba46b3f14bb952e0454d113a9e962e3,0xb55dbcf95230c4d538bb9a79f058c85866100b51e743b5718c18a98431defcbc,0xacf70391bc56ab8c161f1384dd02a06164bb34175aea903301cad07521a689be,0xecb39d7dc660efa75d7a18a8d4db3e2e3a0da19d3352e5f18b64568eb0c1c663,0xebc254ae35aa20b0c18ee03c2f4979854132f4e7cb3a9e51535aa01e1d692956,0xac98fa397c3052a821983e5450be3ef992340ca7882f2e1ecc7ad181ef246b2b,0xf7d0de1c4e21b01cf62c0f8bc059123a0d414bd31a11a6dfc8e23f9bd01cbf7a,0x6a6a60c0747278a0cb5bc926050d07ca3b01db64a4c6236492a6d4eddb8ae173,0x79540a1a839d3c59e4f31fc696b30e06b42315c18a4ced2de511e78bc209ab23,0x163ab304b20f6457e348e16f33a6315474a05c0ecf5efee0a6b50e22e926cbc0,0x5d52f960b8f952feda2a79cd33913bafc363362860d3d412fcd68a5d8807b8ff,0x7f1681c5664a3eedacd3132c4b80d94f577c0d1f35d135c0655a8a8c80ddf9d3,0x08d59b0a61bf95e1ecff74da8f8256665d86be4b43a736520bee5ee561a75b72,0x562e8d958c3d4a2e1907b81c9e59c3fd935fcb1d9b3c8a5f47a73a1dea02f81c,0x4bb4d0104039c2b0f57e1245be8f38f2b4c6293746605ca618996e6bb360c13c,0x60e7eef1268fb489ab2d40a154e97ba890a7c94e136e3935fad2e5eac928fe73,0x00581663fde691995e949b172fa6335eb92054d2ea586fe32aa9b36d796740f6,0xd611361fdc88b4c1a005d0a52f065fa54c875c8ece720364d4c3c16ea5b47273,0x1526717d618ccae8cd0c70d487b2bd194ae623f5ec2fe46d9154262fa40a44cd,0xd369218dc5af01cd780e1c7728e97cbccca10cb32d225363028862b29d2a4701,0x098087c414127cd5db9b7ebc4e51b790e7cb902c1a8388b0d3408d2db825ac4d,0x1bc90e0504262073de2918d1b0e954fc78ad446566942439bfba254f6498ab19,0x4691620c56ed27a52a7f892524d3a531f8fbad15a6c46ca22b35d085ddd355b3,0x66becec1c0a851fde4c1dfe1e1483dcbcae388432caebf0fe2927d3f09a9d4bf,0x1a306e52051a84271c256ba82e18baa00cd422381e7d45b27e9d6a671027e09c,0x49c961fdbfde4acc32c4aff3bf6e578aae9b5a9da582e19f5dbb7d29ca250339,0x5451b36e51a4ead628a73cea7ab1e2e767344e02fd07c7cce08dda12ea808a49,0x635e1234ed959654dfd5ce5b4a02b13e1979505eb0a4afa4573907e52f3c86a3,0x3b1975bb5d42a8cb0ee26133f94cb2bbed1718ee6c078a627486f41184cf489f,0xeac3cc1a433fa10c59de656fd94d7cdf03546fec7da17754a380196547513e57,0x6773dadb2af40a90df48801cbfbbea74fe6b90f9b0b287c7b2d2ed5fbad54290,0x9cc268d45e1f0b326eac96135d0e27f7282844127a0c4eae56cde23e1ff4edca,0x6ba5a9cbe1566283d76cb50b599640befb80aa91ec41f0224903df7537b16220,0xf2380bc5b1069e9890d0b2ee7b8af31ba30f38db6b39d2c7bd4b035115d20be2,0x1001f5803b4ac3ddfc91a5a11dac3ab7c9ce5d632ffd0221308baa8ded4ef0cb,0x42b9e282e9b8f85d3c573017a8275161167361146d2c3e81f1319272f2c4fad1,0xb6a675ff3ef8b2ae2b41b1afdea56eb260c158012305a6c8bed34f54559ad82c,0xe71840433413e75d5d80fa93db64be08872c1d7e76abbb78423f9d4747423b74,0x3e19acb199d630eebddaa86787b21a718f55479ee5fa83a77e648d65ccffdc86,0x9654e04dae8e588c02e10a15a938ac58942f86db202b4b342d6fdeb2f8b57899,0xefeabd00c8cd3dedfdb17c6f9d886a4a603f3d64675d2a8423503dcdaacc5204`
)

func TestGenesis(t *testing.T) {
	b := NewTestBlockchain(t, nil)

	// add genesis block
	genesis := &types.Header{Difficulty: 1, Number: 0}
	genesis.ComputeHash()

	assert.NoError(t, b.writeGenesisImpl(genesis))

	header := b.Header()
	assert.Equal(t, header.Hash, genesis.Hash)
}

type dummyChain struct {
	headers map[byte]*types.Header
}

func (c *dummyChain) add(h *header) error {
	if _, ok := c.headers[h.hash]; ok {
		return fmt.Errorf("hash already imported")
	}

	var parent types.Hash
	if h.number != 0 {
		p, ok := c.headers[h.parent]
		if !ok {
			return fmt.Errorf("parent not found %v", h.parent)
		}

		parent = p.Hash
	}

	hh := &types.Header{
		ParentHash: parent,
		Number:     h.number,
		Difficulty: h.diff,
		ExtraData:  []byte{h.hash},
	}

	hh.ComputeHash()
	c.headers[h.hash] = hh

	return nil
}

type header struct {
	hash   byte
	parent byte
	number uint64
	diff   uint64
}

func (h *header) Parent(parent byte) *header {
	h.parent = parent
	h.number = uint64(parent) + 1

	return h
}

func (h *header) Diff(d uint64) *header {
	h.diff = d

	return h
}

func (h *header) Number(d uint64) *header {
	h.number = d

	return h
}

func mock(number byte) *header {
	return &header{
		hash:   number,
		parent: number - 1,
		number: uint64(number),
		diff:   uint64(number),
	}
}

func TestInsertHeaders(t *testing.T) {
	type evnt struct {
		NewChain []*header
		OldChain []*header
		Diff     *big.Int
	}

	type headerEvnt struct {
		header *header
		event  *evnt
	}

	var cases = []struct {
		Name    string
		History []*headerEvnt
		Head    *header
		Forks   []*header
		Chain   []*header
		TD      uint64
	}{
		{
			Name: "Genesis",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
			},
			Head: mock(0x0),
			Chain: []*header{
				mock(0x0),
			},
			TD: 0,
		},
		{
			Name: "Linear",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(3),
					},
				},
			},
			Head: mock(0x2),
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x2),
			},
			TD: 0 + 1 + 2,
		},
		{
			Name: "Keep block with higher difficulty",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x3).Parent(0x1).Diff(5),
					event: &evnt{
						NewChain: []*header{
							mock(0x3).Parent(0x1).Diff(5),
						},
						Diff: big.NewInt(6),
					},
				},
				{
					// This block has lower difficulty than the current chain (fork)
					header: mock(0x2).Parent(0x1).Diff(3),
					event: &evnt{
						OldChain: []*header{
							mock(0x2).Parent(0x1).Diff(3),
						},
					},
				},
			},
			Head:  mock(0x3),
			Forks: []*header{mock(0x2)},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x3).Parent(0x1).Diff(5),
			},
			TD: 0 + 1 + 5,
		},
		{
			Name: "Reorg",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(1 + 2),
					},
				},
				{
					header: mock(0x3),
					event: &evnt{
						NewChain: []*header{
							mock(0x3),
						},
						Diff: big.NewInt(1 + 2 + 3),
					},
				},
				{
					// First reorg
					header: mock(0x4).Parent(0x1).Diff(10).Number(2),
					event: &evnt{
						// add block 4
						NewChain: []*header{
							mock(0x4).Parent(0x1).Diff(10).Number(2),
						},
						// remove block 2 and 3
						OldChain: []*header{
							mock(0x2),
							mock(0x3),
						},
						Diff: big.NewInt(1 + 10),
					},
				},
				{
					header: mock(0x5).Parent(0x4).Diff(11).Number(3),
					event: &evnt{
						NewChain: []*header{
							mock(0x5).Parent(0x4).Diff(11).Number(3),
						},
						Diff: big.NewInt(1 + 10 + 11),
					},
				},
				{
					header: mock(0x6).Parent(0x3).Number(4),
					event: &evnt{
						// lower difficulty, its a fork
						OldChain: []*header{
							mock(0x6).Parent(0x3).Number(4),
						},
					},
				},
			},
			Head:  mock(0x5),
			Forks: []*header{mock(0x6)},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x4).Parent(0x1).Diff(10).Number(2),
				mock(0x5).Parent(0x4).Diff(11).Number(3),
			},
			TD: 0 + 1 + 10 + 11,
		},
		{
			Name: "Forks in reorgs",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(1 + 2),
					},
				},
				{
					header: mock(0x3),
					event: &evnt{
						NewChain: []*header{
							mock(0x3),
						},
						Diff: big.NewInt(1 + 2 + 3),
					},
				},
				{
					// fork 1. 0x1 -> 0x2 -> 0x4
					header: mock(0x4).Parent(0x2).Diff(11),
					event: &evnt{
						NewChain: []*header{
							mock(0x4).Parent(0x2).Diff(11),
						},
						OldChain: []*header{
							mock(0x3),
						},
						Diff: big.NewInt(1 + 2 + 11),
					},
				},
				{
					// fork 2. 0x1 -> 0x2 -> 0x3 -> 0x5
					header: mock(0x5).Parent(0x3),
					event: &evnt{
						OldChain: []*header{
							mock(0x5).Parent(0x3),
						},
					},
				},
				{
					// fork 3. 0x1 -> 0x2 -> 0x6
					header: mock(0x6).Parent(0x2).Diff(5),
					event: &evnt{
						OldChain: []*header{
							mock(0x6).Parent(0x2).Diff(5),
						},
					},
				},
			},
			Head:  mock(0x4),
			Forks: []*header{mock(0x5), mock(0x6)},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x2),
				mock(0x4).Parent(0x2).Diff(11),
			},
			TD: 0 + 1 + 2 + 11,
		},
		{
			Name: "Head from old long fork",
			History: []*headerEvnt{
				{
					header: mock(0x0),
				},
				{
					header: mock(0x1),
					event: &evnt{
						NewChain: []*header{
							mock(0x1),
						},
						Diff: big.NewInt(1),
					},
				},
				{
					header: mock(0x2),
					event: &evnt{
						NewChain: []*header{
							mock(0x2),
						},
						Diff: big.NewInt(1 + 2),
					},
				},
				{
					// fork 1.
					header: mock(0x3).Parent(0x0).Diff(5),
					event: &evnt{
						NewChain: []*header{
							mock(0x3).Parent(0x0).Diff(5),
						},
						OldChain: []*header{
							mock(0x1),
							mock(0x2),
						},
						Diff: big.NewInt(0 + 5),
					},
				},
				{
					// Add back the 0x2 fork
					header: mock(0x4).Parent(0x2).Diff(10),
					event: &evnt{
						NewChain: []*header{
							mock(0x4).Parent(0x2).Diff(10),
							mock(0x2),
							mock(0x1),
						},
						OldChain: []*header{
							mock(0x3).Parent(0x0).Diff(5),
						},
						Diff: big.NewInt(1 + 2 + 10),
					},
				},
			},
			Head: mock(0x4).Parent(0x2).Diff(10),
			Forks: []*header{
				mock(0x2),
				mock(0x3).Parent(0x0).Diff(5),
			},
			Chain: []*header{
				mock(0x0),
				mock(0x1),
				mock(0x2),
				mock(0x4).Parent(0x2).Diff(10),
			},
			TD: 0 + 1 + 2 + 10,
		},
	}

	for _, cc := range cases {
		t.Run(cc.Name, func(t *testing.T) {
			b := NewTestBlockchain(t, nil)

			chain := dummyChain{
				headers: map[byte]*types.Header{},
			}
			for _, i := range cc.History {
				if err := chain.add(i.header); err != nil {
					t.Fatal(err)
				}
			}

			checkEvents := func(a []*header, b []*types.Header) {
				if len(a) != len(b) {
					t.Fatal("bad size")
				}
				for indx := range a {
					if chain.headers[a[indx].hash].Hash != b[indx].Hash {
						t.Fatal("bad")
					}
				}
			}

			// genesis is 0x0
			if err := b.writeGenesisImpl(chain.headers[0x0]); err != nil {
				t.Fatal(err)
			}

			// we need to subscribe just after the genesis and history
			sub := b.SubscribeEvents()

			// run the history
			for i := 1; i < len(cc.History); i++ {
				headers := []*types.Header{chain.headers[cc.History[i].header.hash]}
				if err := b.WriteHeadersWithBodies(headers); err != nil {
					t.Fatal(err)
				}

				// get the event
				evnt := sub.GetEvent()
				checkEvents(cc.History[i].event.NewChain, evnt.NewChain)
				checkEvents(cc.History[i].event.OldChain, evnt.OldChain)

				if evnt.Difficulty != nil {
					if evnt.Difficulty.Cmp(cc.History[i].event.Diff) != 0 {
						t.Fatal("bad diff in event")
					}
				}
			}

			head := b.Header()

			expected, ok := chain.headers[cc.Head.hash]
			assert.True(t, ok)

			// check that we got the right hash
			assert.Equal(t, head.Hash, expected.Hash)

			forks, err := b.GetForks()
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				t.Fatal(err)
			}

			expectedForks := []types.Hash{}

			for _, i := range cc.Forks {
				expectedForks = append(expectedForks, chain.headers[i.hash].Hash)
			}

			if len(forks) != 0 {
				if len(forks) != len(expectedForks) {
					t.Fatalf("forks length dont match, expected %d but found %d", len(expectedForks), len(forks))
				} else {
					if !reflect.DeepEqual(forks, expectedForks) {
						t.Fatal("forks dont match")
					}
				}
			}

			// Check chain of forks
			if cc.Chain != nil {
				for indx, i := range cc.Chain {
					block, _ := b.GetBlockByNumber(uint64(indx), true)
					if block.Hash().String() != chain.headers[i.hash].Hash.String() {
						t.Fatal("bad")
					}
				}
			}

			if td, _ := b.GetChainTD(); cc.TD != td.Uint64() {
				t.Fatal("bad")
			}
		})
	}
}

func TestForkUnknownParents(t *testing.T) {
	b := NewTestBlockchain(t, nil)

	h0 := NewTestHeaders(10)
	h1 := AppendNewTestHeaders(h0[:5], 10)

	// Write genesis
	batchWriter := storage.NewBatchWriter(b.db)
	td := new(big.Int).SetUint64(h0[0].Difficulty)

	batchWriter.PutCanonicalHeader(h0[0], td)

	assert.NoError(t, b.writeBatchAndUpdate(batchWriter, h0[0], td, true))

	// Write 10 headers
	assert.NoError(t, b.WriteHeadersWithBodies(h0[1:]))

	// Cannot write this header because the father h1[11] is not known
	assert.Error(t, b.WriteHeadersWithBodies([]*types.Header{h1[12]}))
}

func TestBlockchainWriteBody(t *testing.T) {
	t.Parallel()

	var (
		addr = types.StringToAddress("1")
	)

	newChain := func(
		t *testing.T,
		txFromByTxHash map[types.Hash]types.Address,
		path string,
	) *Blockchain {
		t.Helper()

		dbStorage, err := memory.NewMemoryStorage(nil)
		assert.NoError(t, err)

		chain := &Blockchain{
			db: dbStorage,
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		return chain
	}

	t.Run("should succeed if tx has from field", func(t *testing.T) {
		t.Parallel()

		tx := &types.Transaction{
			Value: big.NewInt(10),
			V:     big.NewInt(1),
			From:  addr,
		}

		block := &types.Block{
			Header: &types.Header{},
			Transactions: []*types.Transaction{
				tx,
			},
		}

		tx.ComputeHash()
		block.Header.ComputeHash()

		txFromByTxHash := map[types.Hash]types.Address{}

		chain := newChain(t, txFromByTxHash, "t1")
		defer chain.db.Close()
		batchWriter := storage.NewBatchWriter(chain.db)

		assert.NoError(
			t,
			chain.writeBody(batchWriter, block),
		)
		assert.NoError(t, batchWriter.WriteBatch())
	})

	t.Run("should return error if tx doesn't have from and recovering address fails", func(t *testing.T) {
		t.Parallel()

		tx := &types.Transaction{
			Value: big.NewInt(10),
			V:     big.NewInt(1),
		}

		block := &types.Block{
			Header: &types.Header{},
			Transactions: []*types.Transaction{
				tx,
			},
		}

		tx.ComputeHash()
		block.Header.ComputeHash()

		txFromByTxHash := map[types.Hash]types.Address{}

		chain := newChain(t, txFromByTxHash, "t2")
		defer chain.db.Close()
		batchWriter := storage.NewBatchWriter(chain.db)

		assert.ErrorIs(
			t,
			errRecoveryAddressFailed,
			chain.writeBody(batchWriter, block),
		)
		assert.NoError(t, batchWriter.WriteBatch())
	})

	t.Run("should recover from address and store to storage", func(t *testing.T) {
		t.Parallel()

		tx := &types.Transaction{
			Value: big.NewInt(10),
			V:     big.NewInt(1),
		}

		block := &types.Block{
			Header: &types.Header{},
			Transactions: []*types.Transaction{
				tx,
			},
		}

		tx.ComputeHash()
		block.Header.ComputeHash()

		txFromByTxHash := map[types.Hash]types.Address{
			tx.Hash: addr,
		}

		chain := newChain(t, txFromByTxHash, "t3")
		defer chain.db.Close()
		batchWriter := storage.NewBatchWriter(chain.db)

		batchWriter.PutHeader(block.Header)

		assert.NoError(t, chain.writeBody(batchWriter, block))

		assert.NoError(t, batchWriter.WriteBatch())

		readBody, ok := chain.readBody(block.Hash())
		assert.True(t, ok)

		assert.Equal(t, addr, readBody.Transactions[0].From)
	})
}

func Test_recoverFromFieldsInBlock(t *testing.T) {
	t.Parallel()

	var (
		addr1 = types.StringToAddress("1")
		addr2 = types.StringToAddress("1")
		addr3 = types.StringToAddress("1")
	)

	computeTxHashes := func(txs ...*types.Transaction) {
		for _, tx := range txs {
			tx.ComputeHash()
		}
	}

	t.Run("should succeed", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: addr1}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2)

		txFromByTxHash[tx2.Hash] = addr2

		block := &types.Block{
			Transactions: []*types.Transaction{
				tx1,
				tx2,
			},
		}

		assert.NoError(
			t,
			chain.recoverFromFieldsInBlock(block),
		)
	})

	t.Run("should stop and return error if recovery fails", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: types.ZeroAddress}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}
		tx3 := &types.Transaction{Nonce: 2, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2, tx3)

		// returns only addresses for tx1 and tx3
		txFromByTxHash[tx1.Hash] = addr1
		txFromByTxHash[tx3.Hash] = addr3

		block := &types.Block{
			Transactions: []*types.Transaction{
				tx1,
				tx2,
				tx3,
			},
		}

		assert.ErrorIs(
			t,
			chain.recoverFromFieldsInBlock(block),
			errRecoveryAddressFailed,
		)

		assert.Equal(t, addr1, tx1.From)
		assert.Equal(t, types.ZeroAddress, tx2.From)
		assert.Equal(t, types.ZeroAddress, tx3.From)
	})
}

func Test_recoverFromFieldsInTransactions(t *testing.T) {
	t.Parallel()

	var (
		addr1 = types.StringToAddress("1")
		addr2 = types.StringToAddress("1")
		addr3 = types.StringToAddress("1")
	)

	computeTxHashes := func(txs ...*types.Transaction) {
		for _, tx := range txs {
			tx.ComputeHash()
		}
	}

	t.Run("should succeed", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			logger: hclog.NewNullLogger(),
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: addr1}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2)

		txFromByTxHash[tx2.Hash] = addr2

		transactions := []*types.Transaction{
			tx1,
			tx2,
		}

		assert.True(
			t,
			chain.recoverFromFieldsInTransactions(transactions),
		)
	})

	t.Run("should succeed even though recovery fails for some transactions", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			logger: hclog.NewNullLogger(),
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: types.ZeroAddress}
		tx2 := &types.Transaction{Nonce: 1, From: types.ZeroAddress}
		tx3 := &types.Transaction{Nonce: 2, From: types.ZeroAddress}

		computeTxHashes(tx1, tx2, tx3)

		// returns only addresses for tx1 and tx3
		txFromByTxHash[tx1.Hash] = addr1
		txFromByTxHash[tx3.Hash] = addr3

		transactions := []*types.Transaction{
			tx1,
			tx2,
			tx3,
		}

		assert.True(t, chain.recoverFromFieldsInTransactions(transactions))

		assert.Equal(t, addr1, tx1.From)
		assert.Equal(t, types.ZeroAddress, tx2.From)
		assert.Equal(t, addr3, tx3.From)
	})

	t.Run("should return false if all transactions has from field", func(t *testing.T) {
		t.Parallel()

		txFromByTxHash := map[types.Hash]types.Address{}
		chain := &Blockchain{
			logger: hclog.NewNullLogger(),
			txSigner: &mockSigner{
				txFromByTxHash: txFromByTxHash,
			},
		}

		tx1 := &types.Transaction{Nonce: 0, From: addr1}
		tx2 := &types.Transaction{Nonce: 1, From: addr2}

		computeTxHashes(tx1, tx2)

		txFromByTxHash[tx2.Hash] = addr2

		transactions := []*types.Transaction{
			tx1,
			tx2,
		}

		assert.False(
			t,
			chain.recoverFromFieldsInTransactions(transactions),
		)
	})
}

func TestBlockchainReadBody(t *testing.T) {
	dbStorage, err := memory.NewMemoryStorage(nil)
	assert.NoError(t, err)

	txFromByTxHash := make(map[types.Hash]types.Address)
	addr := types.StringToAddress("1")

	b := &Blockchain{
		logger: hclog.NewNullLogger(),
		db:     dbStorage,
		txSigner: &mockSigner{
			txFromByTxHash: txFromByTxHash,
		},
	}

	batchWriter := storage.NewBatchWriter(b.db)

	tx := &types.Transaction{
		Value: big.NewInt(10),
		V:     big.NewInt(1),
	}

	tx.ComputeHash()

	block := &types.Block{
		Header: &types.Header{},
		Transactions: []*types.Transaction{
			tx,
		},
	}

	block.Header.ComputeHash()

	txFromByTxHash[tx.Hash] = types.ZeroAddress

	batchWriter.PutCanonicalHeader(block.Header, big.NewInt(0))

	require.NoError(t, b.writeBody(batchWriter, block))

	assert.NoError(t, batchWriter.WriteBatch())

	txFromByTxHash[tx.Hash] = addr

	readBody, found := b.readBody(block.Hash())

	assert.True(t, found)
	assert.Equal(t, addr, readBody.Transactions[0].From)
}

func TestCalculateGasLimit(t *testing.T) {
	tests := []struct {
		name             string
		blockGasTarget   uint64
		parentGasLimit   uint64
		expectedGasLimit uint64
	}{
		{
			name:             "should increase next gas limit towards target",
			blockGasTarget:   25000000,
			parentGasLimit:   20000000,
			expectedGasLimit: 20000000/1024 + 20000000,
		},
		{
			name:             "should decrease next gas limit towards target",
			blockGasTarget:   25000000,
			parentGasLimit:   26000000,
			expectedGasLimit: 26000000 - 26000000/1024,
		},
		{
			name:             "should not alter gas limit when exactly the same",
			blockGasTarget:   25000000,
			parentGasLimit:   25000000,
			expectedGasLimit: 25000000,
		},
		{
			name:             "should increase to the exact gas target if adding the delta surpasses it",
			blockGasTarget:   25000000 + 25000000/1024 - 100, // - 100 so that it takes less than the delta to reach it
			parentGasLimit:   25000000,
			expectedGasLimit: 25000000 + 25000000/1024 - 100,
		},
		{
			name:             "should decrease to the exact gas target if subtracting the delta surpasses it",
			blockGasTarget:   25000000 - 25000000/1024 + 100, // + 100 so that it takes less than the delta to reach it
			parentGasLimit:   25000000,
			expectedGasLimit: 25000000 - 25000000/1024 + 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storageCallback := func(storage *storage.MockStorage) {
				storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
					return &types.Header{
						// This is going to be the parent block header
						GasLimit: tt.parentGasLimit,
					}, nil
				})
			}

			b, blockchainErr := NewMockBlockchain(map[TestCallbackType]interface{}{
				StorageCallback: storageCallback,
			})
			if blockchainErr != nil {
				t.Fatalf("unable to construct the blockchain, %v", blockchainErr)
			}

			b.genesisConfig.Params = &chain.Params{
				BlockGasTarget: tt.blockGasTarget,
			}

			nextGas, err := b.CalculateGasLimit(1)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedGasLimit, nextGas)
		})
	}
}

// TestGasPriceAverage tests the average gas price of the
// blockchain
func TestGasPriceAverage(t *testing.T) {
	testTable := []struct {
		name               string
		previousAverage    *big.Int
		previousCount      *big.Int
		newValues          []*big.Int
		expectedNewAverage *big.Int
	}{
		{
			"no previous average data",
			big.NewInt(0),
			big.NewInt(0),
			[]*big.Int{
				big.NewInt(1),
				big.NewInt(2),
				big.NewInt(3),
				big.NewInt(4),
				big.NewInt(5),
			},
			big.NewInt(3),
		},
		{
			"previous average data",
			// For example (5 + 5 + 5 + 5 + 5) / 5
			big.NewInt(5),
			big.NewInt(5),
			[]*big.Int{
				big.NewInt(1),
				big.NewInt(2),
				big.NewInt(3),
			},
			// (5 * 5 + 1 + 2 + 3) / 8
			big.NewInt(3),
		},
	}

	for _, testCase := range testTable {
		t.Run(testCase.name, func(t *testing.T) {
			// Setup the mock data
			blockchain := NewTestBlockchain(t, nil)
			blockchain.gpAverage.price = testCase.previousAverage
			blockchain.gpAverage.count = testCase.previousCount

			// Update the average gas price
			blockchain.updateGasPriceAvg(testCase.newValues)

			// Make sure the average gas price count is correct
			assert.Equal(
				t,
				int64(len(testCase.newValues))+testCase.previousCount.Int64(),
				blockchain.gpAverage.count.Int64(),
			)

			// Make sure the average gas price is correct
			assert.Equal(t, testCase.expectedNewAverage.String(), blockchain.gpAverage.price.String())
		})
	}
}

// TestBlockchain_VerifyBlockParent verifies that parent block verification
// errors are handled correctly
func TestBlockchain_VerifyBlockParent(t *testing.T) {
	t.Parallel()

	emptyHeader := &types.Header{
		Hash:       types.ZeroHash,
		ParentHash: types.ZeroHash,
	}
	emptyHeader.ComputeHash()

	t.Run("Missing parent block", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return nil, errors.New("not found")
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block
		block := &types.Block{
			Header: &types.Header{
				ParentHash: types.ZeroHash,
			},
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrParentNotFound)
	})

	t.Run("Parent hash mismatch", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block whose parent hash will
		// not match the computed parent hash
		block := &types.Block{
			Header: emptyHeader.Copy(),
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrParentHashMismatch)
	})

	t.Run("Invalid block sequence", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block with a number much higher than the parent
		block := &types.Block{
			Header: &types.Header{
				Number: 10,
			},
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrParentHashMismatch)
	})

	t.Run("Invalid block sequence", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block with a number much higher than the parent
		block := &types.Block{
			Header: &types.Header{
				Number:     10,
				ParentHash: emptyHeader.Copy().Hash,
			},
		}

		assert.ErrorIs(t, blockchain.verifyBlockParent(block), ErrInvalidBlockSequence)
	})

	t.Run("Invalid block gas limit", func(t *testing.T) {
		t.Parallel()

		parentHeader := emptyHeader.Copy()
		parentHeader.GasLimit = 5000

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		// Create a dummy block with a number much higher than the parent
		block := &types.Block{
			Header: &types.Header{
				Number:     1,
				ParentHash: parentHeader.Hash,
				GasLimit:   parentHeader.GasLimit + 1000, // The gas limit is greater than the allowed rate
			},
		}

		assert.Error(t, blockchain.verifyBlockParent(block))
	})
}

// TestBlockchain_VerifyBlockBody makes sure that the block body is verified correctly
func TestBlockchain_VerifyBlockBody(t *testing.T) {
	t.Parallel()

	emptyHeader := &types.Header{
		Hash:       types.ZeroHash,
		ParentHash: types.ZeroHash,
	}

	t.Run("Invalid SHA3 Uncles root", func(t *testing.T) {
		t.Parallel()

		blockchain, err := NewMockBlockchain(nil)
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.ZeroHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, ErrInvalidSha3Uncles)
	})

	t.Run("Invalid Transactions root", func(t *testing.T) {
		t.Parallel()

		blockchain, err := NewMockBlockchain(nil)
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, ErrInvalidTxRoot)
	})

	t.Run("Invalid execution result - missing parent", func(t *testing.T) {
		t.Parallel()

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return nil, errors.New("not found")
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback: storageCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
				TxRoot:     types.EmptyRootHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, ErrParentNotFound)
	})

	t.Run("Invalid execution result - unable to fetch block creator", func(t *testing.T) {
		t.Parallel()

		errBlockCreatorNotFound := errors.New("not found")

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			// This is used for parent fetching
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		// Set up the verifier callback
		verifierCallback := func(verifier *MockVerifier) {
			// This is used for error-ing out on the block creator fetch
			verifier.HookGetBlockCreator(func(t *types.Header) (types.Address, error) {
				return types.ZeroAddress, errBlockCreatorNotFound
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback:  storageCallback,
			VerifierCallback: verifierCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
				TxRoot:     types.EmptyRootHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, errBlockCreatorNotFound)
	})

	t.Run("Invalid execution result - unable to execute transactions", func(t *testing.T) {
		t.Parallel()

		errUnableToExecute := errors.New("unable to execute transactions")

		// Set up the storage callback
		storageCallback := func(storage *storage.MockStorage) {
			// This is used for parent fetching
			storage.HookReadHeader(func(hash types.Hash) (*types.Header, error) {
				return emptyHeader.Copy(), nil
			})
		}

		executorCallback := func(executor *mockExecutor) {
			// This is executor processing
			executor.HookProcessBlock(func(
				hash types.Hash,
				block *types.Block,
				address types.Address,
			) (*state.Transition, error) {
				return nil, errUnableToExecute
			})
		}

		blockchain, err := NewMockBlockchain(map[TestCallbackType]interface{}{
			StorageCallback:  storageCallback,
			ExecutorCallback: executorCallback,
		})
		if err != nil {
			t.Fatalf("unable to instantiate new blockchain, %v", err)
		}

		block := &types.Block{
			Header: &types.Header{
				Sha3Uncles: types.EmptyUncleHash,
				TxRoot:     types.EmptyRootHash,
			},
		}

		_, err = blockchain.verifyBlockBody(block)
		assert.ErrorIs(t, err, errUnableToExecute)
	})
}

func TestBlockchain_CalculateBaseFee(t *testing.T) {
	t.Parallel()

	tests := []struct {
		blockNumber          uint64
		parentBaseFee        uint64
		parentGasLimit       uint64
		parentGasUsed        uint64
		elasticityMultiplier uint64
		forks                *chain.Forks
		getLatestConfigFn    getChainConfigDelegate
		expectedBaseFee      uint64
	}{
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      chain.GenesisBaseFee,
		}, // usage == target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1125000000,
		}, // usage == target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        9000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      987500000,
		}, // usage below target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        9000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1100000000,
		}, // usage below target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        11000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      1012500000,
		}, // usage above target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        11000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1150000000,
		}, // usage above target
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        20000000,
			elasticityMultiplier: 2,
			expectedBaseFee:      1125000000,
		}, // usage full
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        20000000,
			elasticityMultiplier: 4,
			expectedBaseFee:      1375000000,
		}, // usage full
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        0,
			elasticityMultiplier: 2,
			expectedBaseFee:      875000000,
		}, // usage 0
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        0,
			elasticityMultiplier: 4,
			expectedBaseFee:      875000000,
		}, // usage 0
		{
			blockNumber:     6,
			forks:           &chain.Forks{chain.London: chain.NewFork(10)},
			expectedBaseFee: 0,
		}, // London hard fork disabled
		{
			blockNumber:     6,
			parentBaseFee:   0,
			expectedBaseFee: 10,
		},
		// first block with London hard fork
		// (return base fee value configured in the genesis)
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 4,
			forks:                chain.AllForksEnabled,
			getLatestConfigFn: func() (*chain.Params, error) {
				return &chain.Params{BaseFeeChangeDenom: 4}, nil
			},
			expectedBaseFee: 1250000000,
		}, // governance hard fork enabled
		{
			blockNumber:          6,
			parentBaseFee:        chain.GenesisBaseFee,
			parentGasLimit:       20000000,
			parentGasUsed:        10000000,
			elasticityMultiplier: 4,
			forks:                chain.AllForksEnabled,
			getLatestConfigFn: func() (*chain.Params, error) {
				return nil, errors.New("failed to retrieve chain config")
			},
			expectedBaseFee: 1000000008,
		}, // governance hard fork enabled
	}

	for i, test := range tests {
		test := test

		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			t.Parallel()

			forks := &chain.Forks{
				chain.London: chain.NewFork(5),
			}

			if test.forks != nil {
				forks = test.forks
			}

			blockchain := &Blockchain{
				logger: hclog.NewNullLogger(),
				genesisConfig: &chain.Chain{
					Params: &chain.Params{
						Forks:              forks,
						BaseFeeChangeDenom: chain.BaseFeeChangeDenom,
						BaseFeeEM:          test.elasticityMultiplier,
					},
					Genesis: &chain.Genesis{
						BaseFee: 10,
					},
				},
			}

			blockchain.setCurrentHeader(&types.Header{
				Number:   test.blockNumber + 1,
				GasLimit: test.parentGasLimit,
				GasUsed:  test.parentGasUsed,
				BaseFee:  test.parentBaseFee,
			}, big.NewInt(1))

			blockchain.SetConsensus(&MockVerifier{getChainConfigFn: test.getLatestConfigFn})

			parent := &types.Header{
				Number:   test.blockNumber,
				GasLimit: test.parentGasLimit,
				GasUsed:  test.parentGasUsed,
				BaseFee:  test.parentBaseFee,
			}

			got := blockchain.CalculateBaseFee(parent)
			assert.Equal(t, test.expectedBaseFee, got, fmt.Sprintf("expected %d, got %d", test.expectedBaseFee, got))
		})
	}
}

func TestBlockchain_WriteFullBlock(t *testing.T) {
	t.Parallel()

	getKey := func(p []byte, k []byte) []byte {
		return append(append(make([]byte, 0, len(p)+len(k)), p...), k...)
	}
	db := map[string][]byte{}
	consensusMock := &MockVerifier{
		processHeadersFn: func(hs []*types.Header) error {
			assert.Len(t, hs, 1)

			return nil
		},
	}

	storageMock := storage.NewMockStorage()
	storageMock.HookNewBatch(func() storage.Batch {
		return memory.NewBatchMemory(db)
	})

	bc := &Blockchain{
		gpAverage: &gasPriceAverage{
			count: new(big.Int),
		},
		logger:    hclog.NewNullLogger(),
		db:        storageMock,
		consensus: consensusMock,
		genesisConfig: &chain.Chain{
			Params: &chain.Params{
				Forks: &chain.Forks{
					chain.London: chain.NewFork(5),
				},
				BaseFeeEM: 4,
			},
			Genesis: &chain.Genesis{},
		},
		stream: newEventStream(),
	}

	bc.headersCache, _ = lru.New(10)
	bc.difficultyCache, _ = lru.New(10)

	existingTD := big.NewInt(1)
	existingHeader := &types.Header{Number: 1}
	header := &types.Header{
		Number: 2,
	}
	receipts := []*types.Receipt{
		{GasUsed: 100},
		{GasUsed: 200},
	}
	tx := &types.Transaction{
		Value: big.NewInt(1),
	}

	tx.ComputeHash()
	header.ComputeHash()
	existingHeader.ComputeHash()
	bc.currentHeader.Store(existingHeader)
	bc.currentDifficulty.Store(existingTD)

	header.ParentHash = existingHeader.Hash
	bc.txSigner = &mockSigner{
		txFromByTxHash: map[types.Hash]types.Address{
			tx.Hash: {1, 2},
		},
	}

	// already existing block write
	err := bc.WriteFullBlock(&types.FullBlock{
		Block: &types.Block{
			Header:       existingHeader,
			Transactions: []*types.Transaction{tx},
		},
		Receipts: receipts,
	}, "polybft")

	require.NoError(t, err)
	require.Equal(t, 0, len(db))
	require.Equal(t, uint64(1), bc.currentHeader.Load().Number)

	// already existing block write
	err = bc.WriteFullBlock(&types.FullBlock{
		Block: &types.Block{
			Header:       header,
			Transactions: []*types.Transaction{tx},
		},
		Receipts: receipts,
	}, "polybft")

	require.NoError(t, err)
	require.Equal(t, 8, len(db))
	require.Equal(t, uint64(2), bc.currentHeader.Load().Number)
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.BODY, header.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.TX_LOOKUP_PREFIX, tx.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.HEADER, header.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.HEAD, storage.HASH))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.CANONICAL, common.EncodeUint64ToBytes(header.Number)))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.DIFFICULTY, header.Hash.Bytes()))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.CANONICAL, common.EncodeUint64ToBytes(header.Number)))])
	require.NotNil(t, db[hex.EncodeToHex(getKey(storage.RECEIPTS, header.Hash.Bytes()))])
}

func TestDiskUsageWriteBatchAndUpdate(t *testing.T) {
	p, err := os.MkdirTemp("", "DiskUsageTest")
	require.NoError(t, err)

	var receipts types.Receipts

	db, err := leveldb.NewLevelDBStorage(
		filepath.Join(p),
		hclog.NewNullLogger(),
	)

	blockchain := &Blockchain{db: db}

	require.NoError(t, os.MkdirAll(p, 0755))

	require.NoError(t, err)

	dirSizeBeforeBlocks, err := DirSize(p)
	require.NoError(t, err)
	t.Logf("DIRSIZE IS: %d", dirSizeBeforeBlocks)

	transactionsSplitString := strings.Split(transactionsString, ",")

	for _, transaction := range transactionsSplitString {
		receipts = append(receipts, createTestReceipt(createTestLogs(25, types.StringToAddress("0x1")), 35614, 153, types.StringToHash(transaction)))
	}

	for i := 0; i < 1000; i++ {
		batchWriter := storage.NewBatchWriter(db)
		block := &types.Block{Header: GetTestHeader(uint64(i))}

		batchWriter.PutHeader(block.Header)
		batchWriter.PutReceipts(block.Hash(), receipts)

		require.NoError(t, blockchain.writeBatchAndUpdate(batchWriter, block.Header, big.NewInt(0), false))
	}

	dirSizeAfterBlocks, err := DirSize(p)
	require.NoError(t, err)
	t.Logf("DIRSIZE IS: %d", dirSizeAfterBlocks)

	db.Close()

	assert.NotEqual(t, dirSizeBeforeBlocks, dirSizeAfterBlocks)
}

func GetTestHeader(index uint64) *types.Header {
	return &types.Header{Hash: types.StringToHash(fmt.Sprintf("0x99fb59f3f0cec0c2041fa3d85f9c998157c7265d8ac1ff1a1dbb072fc3ac8d3d%d", index)),
		ParentHash:   types.StringToHash("0x670954530901d461823d49426ad1bc6163c35f91e4e1b57ec5925795c990a932"),
		Sha3Uncles:   types.StringToHash("0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"),
		Miner:        types.StringToBytes("0x95222290dd7278aa3ddd389cc1e1d165cc4bafe5"),
		StateRoot:    types.StringToHash("0x4baffff1af649a4a1ac3895a4e6b9882b04955733b61941a46a7f313962ac2be"),
		TxRoot:       types.StringToHash("0xc93ac950c3cab224555d3dbe22e7b918fdcc700a1b5e68f776783031a8989ac6"),
		ReceiptsRoot: types.StringToHash("0x9b35c4cbe7b62baa5aa23a511e9300e34fa9a8b9f8bf3a30b3c0dfe5ac0f03c1"),
		Number:       index,
		GasUsed:      16659603,
		GasLimit:     30000000,
		ExtraData:    types.StringToBytes("0x6265617665726275696c642e6f7267"),
		LogsBloom:    types.Bloom(types.StringToBytes("0x5cffdd8375299b5870f8d6ccc731bbc1991a66bddb89060e06acfd029da27f3f33f9770dd9bac7c04ef81b1e585265bd220bcb28eca77f926fa1c4f724ec2f087e52585d656fbb7d7f92732ffd2c2ce0923b58b16df4e8f599c43eddd3fb082adec776b16b36b3a7815c4bd697627c97aaff8078231d4e77d62735ded51d5a904da0a77e069ea7791fab17650b9e223643ee858de3c99ff870672d7368742e7b6fa81b555de4eb4661932ece0e9ed6d21e71de67c1bf8f4af9673347149fc0dcdf5705b33d8e7a4b660e278f46cdb1ed6bcb37267ff122bc0be190ae38b6e06c71fbbf4add86d8dc07a557a02173c5a49cb295f1ca8c055f59ab074d539f9de7")),
		Timestamp:    1705060811,
		Difficulty:   0,
		Nonce:        types.ZeroNonce,
		MixHash:      types.StringToHash("0xe101fdbfe5155c5489eabc80bab08728861695d8c0a66431df2e76a8ae59ac90"),
	}
}

func createTestReceipt(logs []*types.Log, cumulativeGasUsed, gasUsed uint64, txHash types.Hash) *types.Receipt {
	success := types.ReceiptSuccess

	return &types.Receipt{
		Root:              types.ZeroHash,
		CumulativeGasUsed: cumulativeGasUsed,
		Status:            &success,
		LogsBloom:         types.CreateBloom(nil),
		Logs:              logs,
		GasUsed:           gasUsed,
		TxHash:            txHash,
		TransactionType:   types.DynamicFeeTx,
	}
}

func DirSize(path string) (uint64, error) {
	var size uint64

	entries, err := os.ReadDir(path)
	if err != nil {
		return size, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDirSize, err := DirSize(path + "/" + entry.Name())
			if err != nil {
				log.Printf("failed to calculate size of directory %s: %v\n", entry.Name(), err)
			}
			size += subDirSize
		} else {
			fileInfo, err := entry.Info()

			if err != nil {
				log.Printf("failed to get info of file %s: %v\n", entry.Name(), err)
				continue
			}

			size += uint64(fileInfo.Size())
		}
	}
	return size, nil
}

func createTestLogs(logsCount int, address types.Address) []*types.Log {
	logs := make([]*types.Log, 0, logsCount)
	for i := 0; i < logsCount; i++ {
		logs = append(logs, &types.Log{
			Address: address,
			Topics: []types.Hash{
				types.StringToHash("100"),
				types.StringToHash("ABCD"),
			},
			Data: types.StringToBytes(hex.EncodeToString([]byte("Lorem Ipsum Dolor"))),
		})
	}

	return logs
}
