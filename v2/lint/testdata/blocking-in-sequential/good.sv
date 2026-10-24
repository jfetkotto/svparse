module top (input clk, input [3:0] d, output logic [3:0] q, output logic [3:0] sum);
  always_ff @(posedge clk) begin
    automatic int tmp;
    tmp = d[0] + d[1];
    q <= d;
    for (int i = 0; i < 4; i++) sum[i] <= d[i];
  end
  always @(posedge clk) begin : blk
    int count;
    count = 0;
    foreach (d[k]) count = count + d[k];
  end
  always_comb sum = d;
  initial q = 0;
endmodule
